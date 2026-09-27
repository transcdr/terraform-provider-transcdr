package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

var (
	_ resource.Resource                   = (*automationResource)(nil)
	_ resource.ResourceWithConfigure      = (*automationResource)(nil)
	_ resource.ResourceWithImportState    = (*automationResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*automationResource)(nil)
	_ resource.ResourceWithValidateConfig = (*automationResource)(nil)
)

func newAutomationResource() resource.Resource { return &automationResource{} }

type automationResource struct {
	client *transcdr.Client
}

type automationModel struct {
	ID                  types.String            `tfsdk:"id"`
	Name                types.String            `tfsdk:"name"`
	Enabled             types.Bool              `tfsdk:"enabled"`
	Trigger             types.String            `tfsdk:"trigger"`
	TriggerConnectionID types.String            `tfsdk:"trigger_connection_id"`
	Source              *automationSourceModel  `tfsdk:"source"`
	PollIntervalSeconds types.Int64             `tfsdk:"poll_interval_seconds"`
	SettleSeconds       types.Int64             `tfsdk:"settle_seconds"`
	Preset              types.String            `tfsdk:"preset"`
	Output              jsontypes.Normalized    `tfsdk:"output"`
	Destination         *automationDestinations `tfsdk:"destination"`
	AfterSuccess        types.String            `tfsdk:"after_success"`
	Priority            types.String            `tfsdk:"priority"`
	Metadata            types.Map               `tfsdk:"metadata"`
	WebhookURL          types.String            `tfsdk:"webhook_url"`
	HookURL             types.String            `tfsdk:"hook_url"`
	HookTokenVersion    types.Int64             `tfsdk:"hook_token_version"`
	JobsCreated         types.Int64             `tfsdk:"jobs_created"`
	LastPolledAt        types.String            `tfsdk:"last_polled_at"`
	LastTriggeredAt     types.String            `tfsdk:"last_triggered_at"`
	LastError           types.String            `tfsdk:"last_error"`
}

type automationSourceModel struct {
	ConnectionID types.String `tfsdk:"connection_id"`
	Prefix       types.String `tfsdk:"prefix"`
	Pattern      types.String `tfsdk:"pattern"`
}

type automationDestinations struct {
	ConnectionID types.String `tfsdk:"connection_id"`
	Prefix       types.String `tfsdk:"prefix"`
}

func (r *automationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_automation"
}

func (r *automationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An automation: when a file lands in a storage connection, transcode it like this and deliver it there. Needs the Starter plan or above.\n\n" +
			"Three triggers:\n" +
			"- `watch` lists the source every `poll_interval_seconds` and takes files unchanged for `settle_seconds`.\n" +
			"- `hook` takes pushes at the secret `hook_url`: `{\"path\": …}`, `{\"paths\": [...]}`, S3/R2/MinIO bucket notifications (directly or wrapped by SNS, whose subscription it confirms itself) or GCS notifications.\n" +
			"- `queue` consumes an `sqs` connection (`trigger_connection_id`): S3 notifications sent to the queue directly or through an SNS topic, EventBridge `Object Created` events, `{\"path\"}` messages and `POST /v1/jobs` bodies.\n\n" +
			"Each object version is processed exactly once. Use the `transcdr_bucket_automation_setup` data source for the IAM, queue and topic policies and the bucket notification.\n\n" +
			"Every argument updates in place. Removing an optional argument (`destination`, `preset`, `output`, `metadata`, `webhook_url`, `trigger_connection_id`) clears it; " +
			"the automation keeps its id, `hook_url` and the record of the files it has processed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The automation id, `aut_…`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name, up to 120 characters.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 120)},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Whether the automation runs. Default true.",
			},
			"trigger": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("watch"),
				MarkdownDescription: "`watch` (poll), `hook` (push to `hook_url`) or `queue` (consume an SQS connection). Default `watch`. A `watch` source must be listable (`capabilities.watch`).",
				Validators:          []validator.String{stringvalidator.OneOf("watch", "hook", "queue")},
			},
			"trigger_connection_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "`queue` only, and required there: the enabled `sqs` connection to consume. Several queue automations may share a queue; each message goes to all of them.",
			},
			"source": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "Where the files are.",
				Attributes: map[string]schema.Attribute{
					"connection_id": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "A storage connection (`con_…`).",
					},
					"prefix": schema.StringAttribute{
						Optional: true, Computed: true, Default: stringdefault.StaticString(""),
						MarkdownDescription: "Only files under this prefix, relative to the connection's `root`, e.g. `incoming/`. The API stores it normalised (`incoming`); both spellings mean the same here. Default: the whole connection.",
					},
					"pattern": schema.StringAttribute{
						Optional: true, Computed: true,
						MarkdownDescription: "A case-insensitive glob the path must match: `*` within a folder, `**` across folders, `?`, `{a,b}`. The API's default is `**/*.{mp4,mov,mkv,webm,m4v,avi,ts,mts,m2ts,mxf}`.",
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 256)},
						PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
				},
			},
			"poll_interval_seconds": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(300),
				MarkdownDescription: "`watch`: how often the source is listed, 60 to 86400 seconds. Default 300.",
				Validators:          []validator.Int64{int64validator.Between(60, 86400)},
			},
			"settle_seconds": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(60),
				MarkdownDescription: "`watch`: a file is taken once it has been unchanged this long, 0 to 86400 seconds. Default 60.",
				Validators:          []validator.Int64{int64validator.Between(0, 86400)},
			},
			"preset": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "A system preset slug (e.g. `hls-av1-abr`) or a preset id (`pre_…`, e.g. `transcdr_preset.x.id`).",
			},
			"output": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
				MarkdownDescription: "Output specification overrides, as JSON, merged over the preset (`jsonencode({ codec = \"h264\" })`). " +
					"Objects merge; arrays and scalars replace. Compared semantically: formatting and key order do not matter.",
			},
			"destination": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Deliver every output file to a storage connection when the job completes. Omit it to keep outputs in Transcdr's storage; " +
					"removing it stops delivering from the next job on.",
				Attributes: map[string]schema.Attribute{
					"connection_id": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "A writable storage connection (`capabilities.destination`).",
					},
					"prefix": schema.StringAttribute{
						Optional: true, Computed: true,
						MarkdownDescription: "A prefix template: `{job_id}`, `{name}`, `{stem}`, `{ext}`, `{dir}`, `{date}`, `{automation}`, `{org}`. The API's default is `transcdr/{job_id}/`.",
						PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
				},
			},
			"after_success": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("keep"),
				MarkdownDescription: "`keep` or `delete` the source file once its job completes. Default `keep`. `delete` needs `s3:DeleteObject` on the source.",
				Validators:          []validator.String{stringvalidator.OneOf("keep", "delete")},
			},
			"priority": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("normal"),
				MarkdownDescription: "`normal` or `high` (plans with priority). Default `normal`.",
				Validators:          []validator.String{stringvalidator.OneOf("normal", "high")},
			},
			"metadata": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Metadata added to every job (up to 20 keys of ≤ 40 characters, values ≤ 500). Jobs also get `automation_id` and `source_path`.",
			},
			"webhook_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "A per-job webhook URL for every job the automation creates, in addition to the organization's event destinations.",
			},
			"hook_url": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				MarkdownDescription: "The secret push URL for `hook` automations (an SNS HTTPS subscription endpoint, a MinIO webhook target, a script). " +
					"It is the credential: change `hook_token_version` to rotate it. Returned only to keys holding `automations:write`.",
			},
			"hook_token_version": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Any number. Changing it rotates the hook token: `hook_url` changes and the old URL stops working. " +
					"Not sent to the API; setting it on create does not rotate.",
			},
			"jobs_created": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "How many jobs the automation has created.",
			},
			"last_polled_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the source (or queue) was last read.",
			},
			"last_triggered_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the automation last created a job.",
			},
			"last_error": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The last problem the automation hit, e.g. a record from another bucket.",
			},
		},
	}
}

func (r *automationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

var automationParams = topLevelParam(map[string][]string{
	"name": nil, "enabled": nil, "trigger": nil, "trigger_connection_id": nil,
	"source":                {"connection_id", "prefix", "pattern"},
	"poll_interval_seconds": nil, "settle_seconds": nil, "preset": nil, "output": nil,
	"destination":   {"connection_id", "prefix"},
	"after_success": nil, "priority": nil, "metadata": nil, "webhook_url": nil,
})

func (r *automationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg automationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	trigger := cfg.Trigger.ValueString()
	if cfg.Trigger.IsNull() {
		trigger = "watch"
	}
	if cfg.Trigger.IsUnknown() || cfg.TriggerConnectionID.IsUnknown() {
		return
	}
	if trigger == "queue" && cfg.TriggerConnectionID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("trigger_connection_id"), "Missing trigger_connection_id",
			"A queue automation needs the sqs connection to consume: set trigger_connection_id.")
	}
	if trigger != "queue" && !cfg.TriggerConnectionID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("trigger_connection_id"), "trigger_connection_id without a queue trigger",
			"trigger_connection_id applies to trigger = \"queue\" only.")
	}
	if known(cfg.Output) {
		var v any
		if json.Unmarshal([]byte(cfg.Output.ValueString()), &v) == nil {
			if _, ok := v.(map[string]any); !ok {
				resp.Diagnostics.AddAttributeError(path.Root("output"), "Invalid output", "output must be a JSON object.")
			}
		}
	}
}

func (r *automationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state automationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The hook URL changes only when the token is rotated.
	if known(state.HookURL) && plan.HookTokenVersion.Equal(state.HookTokenVersion) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("hook_url"), state.HookURL)...)
	}
}

// params is the create (prior nil) or update request. Updates send every field, with null for an
// optional one that is not set (which clears it), except trigger_connection_id, sent only when it
// changes.
func (m *automationModel) params(ctx context.Context, prior *automationModel, diags *diag.Diagnostics) *transcdr.AutomationParams {
	p := &transcdr.AutomationParams{
		Name:                m.Name.ValueString(),
		Enabled:             transcdr.Bool(m.Enabled.ValueBool()),
		Trigger:             m.Trigger.ValueString(),
		PollIntervalSeconds: transcdr.Int(int(m.PollIntervalSeconds.ValueInt64())),
		SettleSeconds:       transcdr.Int(int(m.SettleSeconds.ValueInt64())),
		AfterSuccess:        m.AfterSuccess.ValueString(),
		Priority:            m.Priority.ValueString(),
		Source: &transcdr.AutomationSourceParams{
			ConnectionID: m.Source.ConnectionID.ValueString(),
			Prefix:       ptr(m.Source.Prefix),
			Pattern:      ptr(m.Source.Pattern),
		},
	}
	// Only on change: naming the queue re-checks that its connection is enabled.
	if prior == nil || !m.TriggerConnectionID.Equal(prior.TriggerConnectionID) {
		p.TriggerConnectionID = orClear(m.TriggerConnectionID, prior != nil)
	}
	p.Preset = orClear(m.Preset, prior != nil)
	p.WebhookURL = orClear(m.WebhookURL, prior != nil)
	switch {
	case known(m.Output):
		p.Output = transcdr.Value(transcdr.RawOutputSpec([]byte(m.Output.ValueString())))
	case prior != nil:
		p.Output = transcdr.Null[*transcdr.OutputSpecInput]()
	}
	switch {
	case m.Destination != nil:
		dest := transcdr.JobDestination{ConnectionID: m.Destination.ConnectionID.ValueString()}
		if known(m.Destination.Prefix) {
			dest.Prefix = m.Destination.Prefix.ValueString()
		}
		p.Destination = transcdr.Value(dest)
	case prior != nil:
		p.Destination = transcdr.Null[transcdr.JobDestination]()
	}
	switch {
	case known(m.Metadata):
		p.Metadata = transcdr.Value(transcdr.Metadata(mapStrings(ctx, m.Metadata, diags)))
	case prior != nil:
		p.Metadata = transcdr.Null[transcdr.Metadata]()
	}
	return p
}

// orClear is v's value, or on an update (clear) null when v is not set.
func orClear(v types.String, clear bool) transcdr.Nullable[string] {
	switch {
	case known(v):
		return transcdr.Value(v.ValueString())
	case clear:
		return transcdr.Null[string]()
	}
	return transcdr.Nullable[string]{}
}

func (r *automationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan automationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	params := plan.params(ctx, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Automations.Create(ctx, params)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the automation", err, automationParams)
		return
	}
	state := plan
	state.fromAPI(a, &plan, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *automationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state automationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Automations.Get(ctx, state.ID.ValueString())
	if transcdr.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the automation", err, nil)
		return
	}
	prior := state
	state.fromAPI(a, &prior, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *automationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state automationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	params := plan.params(ctx, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	a, err := r.client.Automations.Update(ctx, id, params)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the automation", err, automationParams)
		return
	}
	if !plan.HookTokenVersion.Equal(state.HookTokenVersion) {
		if a, err = r.client.Automations.RotateHookToken(ctx, id); err != nil {
			addAPIError(&resp.Diagnostics, "Could not rotate the automation's hook token", err, nil)
			return
		}
	}
	next := plan
	next.ID = state.ID
	next.fromAPI(a, &plan, true)
	if a.HookURL == nil {
		next.HookURL = state.HookURL
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *automationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state automationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := ignoreNotFound(r.client.Automations.Delete(ctx, state.ID.ValueString())); err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the automation", err, nil)
	}
}

func (r *automationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// fromAPI copies an automation from the API into the model. prior is the plan after a write or the
// state on refresh: values that mean the same keep their spelling, and "none" stays null.
func (m *automationModel) fromAPI(a *transcdr.Automation, prior *automationModel, afterWrite bool) {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Enabled = types.BoolValue(a.Enabled)
	m.Trigger = types.StringValue(a.Trigger)
	m.TriggerConnectionID = strOrNull(a.TriggerConnectionID)

	src := automationSourceModel{Prefix: types.StringNull(), Pattern: types.StringNull()}
	if prior.Source != nil {
		src = *prior.Source
	}
	m.Source = &automationSourceModel{
		ConnectionID: types.StringValue(a.Source.ConnectionID),
		Prefix:       keepIfEquivalent(src.Prefix, &a.Source.Prefix, samePath),
		Pattern:      types.StringValue(a.Source.Pattern),
	}
	m.PollIntervalSeconds = types.Int64Value(int64(a.PollIntervalSeconds))
	m.SettleSeconds = types.Int64Value(int64(a.SettleSeconds))
	m.Preset = strOrNull(a.Preset)

	switch {
	case isEmptyJSONObject(a.Output.Raw()) && prior.Output.IsNull():
		m.Output = jsontypes.NewNormalizedNull()
	case known(prior.Output) && jsonEqual(prior.Output.ValueString(), string(a.Output.Raw())):
		m.Output = prior.Output
	default:
		m.Output = jsontypes.NewNormalizedValue(compactJSON(a.Output.Raw()))
	}

	if a.Destination != nil {
		dp := types.StringNull()
		if prior.Destination != nil {
			dp = prior.Destination.Prefix
		}
		m.Destination = &automationDestinations{
			ConnectionID: types.StringValue(a.Destination.ConnectionID),
			Prefix:       keepIfEquivalent(dp, &a.Destination.Prefix, sameString),
		}
	} else {
		m.Destination = nil
	}
	m.AfterSuccess = types.StringValue(a.AfterSuccess)
	m.Priority = types.StringValue(a.Priority)
	m.Metadata = metadataValue(prior.Metadata, a.Metadata)
	m.WebhookURL = keepIfEquivalent(prior.WebhookURL, a.WebhookURL, sameURL)
	if a.HookURL != nil {
		m.HookURL = types.StringValue(*a.HookURL)
	} else if !afterWrite || !known(m.HookURL) {
		// Keys without automations:write do not see the hook URL; keep the one we have.
		m.HookURL = prior.HookURL
		if m.HookURL.IsUnknown() {
			m.HookURL = types.StringNull()
		}
	}
	m.HookTokenVersion = prior.HookTokenVersion
	m.JobsCreated = types.Int64Value(a.JobsCreated)
	m.LastPolledAt = timeOrNull(a.LastPolledAt)
	m.LastTriggeredAt = timeOrNull(a.LastTriggeredAt)
	m.LastError = strOrNull(a.LastError)
}
