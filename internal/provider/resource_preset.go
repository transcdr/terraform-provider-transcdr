package provider

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

var (
	_ resource.Resource                   = (*presetResource)(nil)
	_ resource.ResourceWithConfigure      = (*presetResource)(nil)
	_ resource.ResourceWithImportState    = (*presetResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*presetResource)(nil)
	_ resource.ResourceWithValidateConfig = (*presetResource)(nil)
)

func newPresetResource() resource.Resource { return &presetResource{} }

type presetResource struct {
	client *transcdr.Client
}

type presetModel struct {
	ID             types.String         `tfsdk:"id"`
	Name           types.String         `tfsdk:"name"`
	Slug           types.String         `tfsdk:"slug"`
	Description    types.String         `tfsdk:"description"`
	Output         jsontypes.Normalized `tfsdk:"output"`
	Metadata       types.Map            `tfsdk:"metadata"`
	Version        types.Int64          `tfsdk:"version"`
	ResolvedOutput types.String         `tfsdk:"resolved_output"`
}

func (r *presetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_preset"
}

func (r *presetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom preset: a named output specification that jobs and automations use by id or slug, or `<slug>@<version>` to pin a version.\n\n" +
			"`output` is the whole specification (output spec v2), written as JSON: `kind` (`video`, `audio` or `image`) and every field that kind, its container, codec and audio handling need " +
			"(`container`, `video`, `audio`, `image`, `renditions`, `subtitles`, `trim`, `privacy`). Nothing has a default; a value that follows the source is written out " +
			"(`\"source\"`, `\"standard\"`, `\"from_color\"`, `\"by_size\"`, `\"poster\"`, `\"segment\"`). A missing field is reported at plan time, every one at once.\n\n" +
			"Presets are versioned: a changed `output` adds a version (`version`), and earlier versions never change. " +
			"Terraform compares the fields you set against the preset's resolved specification (in `resolved_output`): a privacy preset matches the four categories it stands for, " +
			"and values the API writes out beside yours (each size's effective rate) are not drift.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The preset id, `pre_…`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name, up to 120 characters.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 120)},
			},
			"slug": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "1 to 64 characters of `a-z`, `0-9` and `-`, unique in the organization and not a system preset's. Made from the name when omitted.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 64)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "A description, up to 1000 characters.",
			},
			"output": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Required:   true,
				MarkdownDescription: "The whole output specification as JSON (`jsonencode({...})`), sent as written and validated against the plan's limits. " +
					"Every change updates the preset in place, as a new version.",
			},
			"metadata": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Up to 20 string keys (≤ 40 characters) with string values (≤ 500).",
			},
			"version": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The preset's latest version. A changed `output` adds one; pin it with `\"${transcdr_preset.x.slug}@${transcdr_preset.x.version}\"`.",
			},
			"resolved_output": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The full output specification the preset resolves to, as JSON.",
			},
		},
	}
}

func (r *presetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

var presetParams = func(param string) (path.Path, bool) {
	root := strings.SplitN(param, ".", 2)[0]
	switch root {
	case "name", "slug", "description", "metadata":
		return path.Root(root), true
	case "":
		return path.Root("output"), true
	}
	// Specification errors name the spec field (`output.renditions.sizes.0.width`).
	return path.Root("output"), true
}

func (r *presetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var output jsontypes.Normalized
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("output"), &output)...)
	if !known(output) {
		return
	}
	var v any
	if json.Unmarshal([]byte(output.ValueString()), &v) == nil {
		if _, ok := v.(map[string]any); !ok {
			resp.Diagnostics.AddAttributeError(path.Root("output"), "Invalid output", "output must be a JSON object.")
			return
		}
		checkOutput(&resp.Diagnostics, path.Root("output"), output.ValueString())
	}
}

func (r *presetResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state presetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !known(plan.Output) || !known(state.Output) {
		return
	}
	// The version and the resolved specification change only with the output.
	if jsonEqual(plan.Output.ValueString(), state.Output.ValueString()) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("resolved_output"), state.ResolvedOutput)...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("version"), state.Version)...)
	}
}

func (r *presetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan presetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	params := presetWrite{
		Name:        plan.Name.ValueString(),
		Slug:        ptr(plan.Slug),
		Description: plan.Description.ValueString(),
		Output:      json.RawMessage(plan.Output.ValueString()),
	}
	if known(plan.Metadata) {
		params.Metadata = mapStrings(ctx, plan.Metadata, &resp.Diagnostics)
	}
	checkOutput(&resp.Diagnostics, path.Root("output"), plan.Output.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	var p transcdr.Preset
	err := r.client.Do(ctx, "POST", "/v1/presets", &params, &p, transcdr.WithIdempotencyKey(transcdr.NewIdempotencyKey()))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the preset", err, presetParams)
		return
	}
	state := plan
	state.fromAPI(&p, &plan, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *presetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state presetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.Presets.Get(ctx, state.ID.ValueString())
	if transcdr.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the preset", err, nil)
		return
	}
	if p.System {
		resp.Diagnostics.AddError("Not a custom preset", p.Slug+" is a system preset. Use the transcdr_preset data source to read it.")
		return
	}
	prior := state
	state.fromAPI(p, &prior, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *presetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state presetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// PUT replaces the whole preset: the output is the whole specification, and a changed one is a
	// new version.
	params := presetWrite{
		Name:        plan.Name.ValueString(),
		Slug:        ptr(plan.Slug),
		Description: plan.Description.ValueString(),
		Output:      json.RawMessage(plan.Output.ValueString()),
	}
	if known(plan.Metadata) {
		params.Metadata = mapStrings(ctx, plan.Metadata, &resp.Diagnostics)
	}
	checkOutput(&resp.Diagnostics, path.Root("output"), plan.Output.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	var p transcdr.Preset
	err := r.client.Do(ctx, "PUT", "/v1/presets/"+url.PathEscape(state.ID.ValueString()), &params, &p)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the preset", err, presetParams)
		return
	}
	next := plan
	next.fromAPI(&p, &plan, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *presetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state presetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := ignoreNotFound(r.client.Presets.Delete(ctx, state.ID.ValueString())); err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the preset", err, nil)
	}
}

// ImportState takes a preset id (`pre_…`) or the organization's slug for it.
func (r *presetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// presetWrite is a preset create (POST) or replace (PUT) with the output sent exactly as written:
// through the SDK's typed spec, fields it does not know would be dropped and show as drift.
type presetWrite struct {
	Name        string            `json:"name"`
	Slug        *string           `json:"slug,omitempty"`
	Description string            `json:"description"`
	Output      json.RawMessage   `json:"output"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// fromAPI copies a preset from the API. Right after a write, output stays as configured (the API
// accepted it); on refresh it is the configured fields' current values (see projectedOutput).
func (m *presetModel) fromAPI(p *transcdr.Preset, prior *presetModel, afterWrite bool) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.Slug = types.StringValue(p.Slug)
	m.Description = types.StringValue(p.Description)
	if known(prior.Output) && afterWrite {
		m.Output = prior.Output
	} else if known(prior.Output) {
		out, _ := projectedOutput(prior.Output.ValueString(), p.Output.Raw())
		if out == prior.Output.ValueString() {
			m.Output = prior.Output
		} else {
			m.Output = jsontypes.NewNormalizedValue(out)
		}
	} else {
		m.Output = jsontypes.NewNormalizedValue(compactJSON(p.Output.Raw()))
	}
	m.Metadata = metadataValue(prior.Metadata, p.Metadata)
	m.Version = types.Int64Value(int64(p.Version))
	m.ResolvedOutput = types.StringValue(compactJSON(p.Output.Raw()))
}
