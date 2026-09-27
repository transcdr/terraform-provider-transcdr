package provider

import (
	"context"
	"encoding/json"
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

	"github.com/transcdr/terraform-provider-transcdr/internal/client"
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
	client *client.Client
}

type presetModel struct {
	ID             types.String         `tfsdk:"id"`
	Name           types.String         `tfsdk:"name"`
	Slug           types.String         `tfsdk:"slug"`
	Description    types.String         `tfsdk:"description"`
	Output         jsontypes.Normalized `tfsdk:"output"`
	Metadata       types.Map            `tfsdk:"metadata"`
	ResolvedOutput types.String         `tfsdk:"resolved_output"`
}

func (r *presetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_preset"
}

func (r *presetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom preset: a named output specification that jobs and automations use by id or slug.\n\n" +
			"`output` is the specification you want, written as JSON and merged over the defaults (`mode`, `codec`, `renditions` or `ladder`, " +
			"`quality` including `target = \"cbr\"` with `bitrate` and `buffer_ms`, `gop`, `segment_seconds`, `audio`, `subtitles`, `color`, `bit_depth`, `max_fps`, `filters`, `trim`). " +
			"Terraform compares only the fields you set against the preset's resolved specification (in `resolved_output`), so defaults filled in by the API are not drift.",
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
				MarkdownDescription: "The output specification as JSON (`jsonencode({...})`), merged over the defaults and validated against the plan's limits. " +
					"Changing a value updates the preset in place. Removing a field you had set replaces the preset (new id; references by slug keep working), " +
					"because the API merges updates into the stored specification and would keep the old value.",
			},
			"metadata": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Up to 20 string keys (≤ 40 characters) with string values (≤ 500).",
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
	// Specification errors name the spec field (`renditions.0.width`, `quality.bitrate`).
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
		}
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
	if jsonEqual(plan.Output.ValueString(), state.Output.ValueString()) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("resolved_output"), state.ResolvedOutput)...)
		return
	}
	if removed := removedJSONPaths(state.Output.ValueString(), plan.Output.ValueString()); len(removed) > 0 {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("output"))
	}
}

func (r *presetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan presetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
		"output":      json.RawMessage(plan.Output.ValueString()),
	}
	if known(plan.Slug) {
		body["slug"] = plan.Slug.ValueString()
	}
	if known(plan.Metadata) {
		body["metadata"] = mapStrings(ctx, plan.Metadata, &resp.Diagnostics)
	}
	var p client.Preset
	if err := r.client.Create(ctx, "/v1/presets", body, &p); err != nil {
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
	var p client.Preset
	err := r.client.Get(ctx, "/v1/presets/"+client.PathEscape(state.ID.ValueString()), nil, &p)
	if client.IsNotFound(err) {
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
	state.fromAPI(&p, &prior, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *presetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state presetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
		"metadata":    mapStrings(ctx, plan.Metadata, &resp.Diagnostics),
	}
	if known(plan.Slug) {
		body["slug"] = plan.Slug.ValueString()
	}
	if !jsonEqual(plan.Output.ValueString(), state.Output.ValueString()) {
		body["output"] = json.RawMessage(plan.Output.ValueString())
	}
	var p client.Preset
	if err := r.client.Patch(ctx, "/v1/presets/"+client.PathEscape(state.ID.ValueString()), body, &p); err != nil {
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
	if err := r.client.Delete(ctx, "/v1/presets/"+client.PathEscape(state.ID.ValueString())); err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the preset", err, nil)
	}
}

// ImportState takes a preset id (`pre_…`) or the organization's slug for it.
func (r *presetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// fromAPI copies a preset from the API. Right after a write, output stays as configured (the API
// accepted it); on refresh it is the configured fields' current values (see projectedOutput).
func (m *presetModel) fromAPI(p *client.Preset, prior *presetModel, afterWrite bool) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.Slug = types.StringValue(p.Slug)
	m.Description = types.StringValue(p.Description)
	if known(prior.Output) && afterWrite {
		m.Output = prior.Output
	} else if known(prior.Output) {
		out, _ := projectedOutput(prior.Output.ValueString(), p.Output)
		if out == prior.Output.ValueString() {
			m.Output = prior.Output
		} else {
			m.Output = jsontypes.NewNormalizedValue(out)
		}
	} else {
		m.Output = jsontypes.NewNormalizedValue(compactJSON(p.Output))
	}
	m.Metadata = metadataValue(prior.Metadata, p.Metadata)
	m.ResolvedOutput = types.StringValue(compactJSON(p.Output))
}
