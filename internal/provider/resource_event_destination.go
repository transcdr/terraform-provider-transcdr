package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/transcdr/terraform-provider-transcdr/internal/client"
	"github.com/transcdr/terraform-provider-transcdr/internal/setup"
)

var (
	_ resource.Resource                     = (*eventDestinationResource)(nil)
	_ resource.ResourceWithConfigure        = (*eventDestinationResource)(nil)
	_ resource.ResourceWithImportState      = (*eventDestinationResource)(nil)
	_ resource.ResourceWithModifyPlan       = (*eventDestinationResource)(nil)
	_ resource.ResourceWithConfigValidators = (*eventDestinationResource)(nil)
	_ resource.ResourceWithValidateConfig   = (*eventDestinationResource)(nil)
)

// eventTypes are the event types an endpoint can subscribe to, besides "*".
var eventTypes = []string{
	"job.created", "job.scheduled", "job.started", "job.completed", "job.failed", "job.canceled",
	"job.delivered", "job.delivery_failed", "asset.ready", "asset.deleted", "automation.triggered",
	"connection.disabled", "webhook.test",
}

func newEventDestinationResource() resource.Resource { return &eventDestinationResource{} }

type eventDestinationResource struct {
	client *client.Client
}

type eventDestinationModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	URL           types.String `tfsdk:"url"`
	TopicARN      types.String `tfsdk:"topic_arn"`
	QueueURL      types.String `tfsdk:"queue_url"`
	AWS           *awsModel    `tfsdk:"aws"`
	ConnectionID  types.String `tfsdk:"connection_id"`
	Events        types.Set    `tfsdk:"events"`
	Description   types.String `tfsdk:"description"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	SigningSecret types.String `tfsdk:"signing_secret"`
	SecretVersion types.Int64  `tfsdk:"secret_version"`
	Target        types.String `tfsdk:"target"`
	FailureCount  types.Int64  `tfsdk:"failure_count"`
}

type awsModel struct {
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	Region          types.String `tfsdk:"region"`
	Endpoint        types.String `tfsdk:"endpoint"`
	MessageGroupID  types.String `tfsdk:"message_group_id"`
}

func (r *eventDestinationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_event_destination"
}

func (r *eventDestinationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An event destination (a webhook endpoint, `/v1/webhooks`): where Transcdr sends events such as `job.completed`. " +
			"It targets an HTTPS URL (`url`), an Amazon SNS topic (`topic_arn` + `aws`) or an Amazon SQS queue (`queue_url` + `aws`), " +
			"or names a messaging connection (`connection_id`) that holds the target and credentials. Needs a plan with the `webhooks` feature.\n\n" +
			"Every delivery is signed with `signing_secret`: HTTPS in the `Transcdr-Signature` header, SNS and SQS in the `transcdr-signature` message attribute.\n\n" +
			"`aws.secret_access_key` is write-only: Terraform keeps the configured value and sends it again only when it changes, which is how you rotate it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The endpoint id, `whk_…`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "`https`, `sns` or `sqs`. Read from the target (`url`, `topic_arn` or `queue_url`) or the connection's kind when omitted. It cannot change: a new type replaces the endpoint.",
				Validators:          []validator.String{stringvalidator.OneOf("https", "sns", "sqs")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "`https`: the public https URL events are POSTed to.",
			},
			"topic_arn": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "`sns`: the topic ARN (`.fifo` for FIFO topics). The key needs `sns:Publish` on it.",
			},
			"queue_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "`sqs`: the queue URL (`.fifo` for FIFO queues). The key needs `sqs:SendMessage` on it.",
			},
			"aws": schema.SingleNestedAttribute{
				Optional:            true,
				MarkdownDescription: "`sns` and `sqs` targets: the credentials and settings to send with.",
				Attributes: map[string]schema.Attribute{
					"access_key_id": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "The access key ID.",
					},
					"secret_access_key": schema.StringAttribute{
						Required: true, Sensitive: true,
						MarkdownDescription: "The secret access key. Write-only: change it to rotate.",
					},
					"region": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Read from the topic ARN or queue URL when omitted.",
					},
					"endpoint": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "`sns` only: an SNS-compatible service endpoint instead of AWS's. (An SQS queue URL is its own endpoint.) Removing it replaces the endpoint, since the API cannot clear it.",
						PlanModifiers:       []planmodifier.String{requiresReplaceWhenRemoved()},
					},
					"message_group_id": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "FIFO topics and queues: the message group (the API's default is `transcdr`). Removing it replaces the endpoint, since the API cannot clear it.",
						PlanModifiers:       []planmodifier.String{requiresReplaceWhenRemoved()},
					},
				},
			},
			"connection_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Deliver through an `sqs`, `sns` or `webhook` connection instead of a target of its own. The connection's health and `enabled` flag then apply. Changing it replaces the endpoint.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"events": schema.SetAttribute{
				Optional: true, Computed: true,
				ElementType:         types.StringType,
				Default:             setdefault.StaticValue(stringSet([]string{"*"})),
				MarkdownDescription: "The event types to send, or `[\"*\"]` for all (the default): `" + joinTicks(eventTypes) + "`.",
				Validators:          []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(append([]string{"*"}, eventTypes...)...))},
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "A description, up to 500 characters.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(500)},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Whether events are sent. Default true.",
			},
			"signing_secret": schema.StringAttribute{
				Computed: true, Sensitive: true,
				MarkdownDescription: "The `whsec_…` secret deliveries are signed with. Returned only when the endpoint is created or its secret rotated, " +
					"so it is empty after an import until `secret_version` changes.",
			},
			"secret_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Any number. Changing it rotates `signing_secret`; the old secret stops working at once. Not sent to the API; setting it on create does not rotate.",
			},
			"target": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Where events go: the HTTPS URL, topic ARN or queue URL, including a connection's.",
			},
			"failure_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Failed deliveries in a row.",
			},
		},
	}
}

func (r *eventDestinationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *eventDestinationResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("url"), path.MatchRoot("topic_arn"), path.MatchRoot("queue_url"), path.MatchRoot("connection_id")),
		resourcevalidator.Conflicting(path.MatchRoot("connection_id"), path.MatchRoot("aws")),
		resourcevalidator.Conflicting(path.MatchRoot("url"), path.MatchRoot("aws")),
	}
}

func (r *eventDestinationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg eventDestinationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if (!cfg.TopicARN.IsNull() || !cfg.QueueURL.IsNull()) && cfg.AWS == nil {
		resp.Diagnostics.AddAttributeError(path.Root("aws"), "Missing aws", "An sns or sqs target needs the aws block with the credentials to send with.")
	}
	if known(cfg.Type) {
		inferred := inferType(&cfg)
		if inferred != "" && inferred != cfg.Type.ValueString() {
			resp.Diagnostics.AddAttributeError(path.Root("type"), "Mismatched type",
				"type = \""+cfg.Type.ValueString()+"\" does not match the target, which is "+inferred+".")
		}
	}
}

// inferType is the endpoint type its target implies, or "" for a connection (whose kind decides).
func inferType(m *eventDestinationModel) string {
	switch {
	case !m.URL.IsNull():
		return "https"
	case !m.TopicARN.IsNull():
		return "sns"
	case !m.QueueURL.IsNull():
		return "sqs"
	}
	return ""
}

func (r *eventDestinationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan eventDestinationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Type.IsUnknown() {
		if t := inferType(&plan); t != "" {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("type"), t)...)
		}
	}
	if req.State.Raw.IsNull() {
		return
	}
	var state eventDestinationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if known(state.Type) {
		if t := inferType(&plan); t != "" && t != state.Type.ValueString() {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("type"))
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("type"), t)...)
		}
	}
	if plan.SecretVersion.Equal(state.SecretVersion) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("signing_secret"), state.SigningSecret)...)
	}
	// The target changes only with url, topic_arn, queue_url or the connection.
	if plan.URL.Equal(state.URL) && plan.TopicARN.Equal(state.TopicARN) && plan.QueueURL.Equal(state.QueueURL) && plan.ConnectionID.Equal(state.ConnectionID) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("target"), state.Target)...)
	}
}

var eventDestinationParams = topLevelParam(map[string][]string{
	"type": nil, "url": nil, "topic_arn": nil, "queue_url": nil, "connection_id": nil, "events": nil, "description": nil, "enabled": nil,
	"aws": {"access_key_id", "secret_access_key", "region", "endpoint", "message_group_id"},
})

func (r *eventDestinationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan eventDestinationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.WebhookInput{
		URL:          ptr(plan.URL),
		TopicARN:     ptr(plan.TopicARN),
		QueueURL:     ptr(plan.QueueURL),
		ConnectionID: ptr(plan.ConnectionID),
		Events:       setStrings(ctx, plan.Events, &resp.Diagnostics),
		Description:  ptr(plan.Description),
	}
	if t := inferType(&plan); t != "" {
		in.Type = &t
	}
	if plan.AWS != nil {
		in.AWS = &client.WebhookAWSInput{
			AccessKeyID:     ptr(plan.AWS.AccessKeyID),
			SecretAccessKey: ptr(plan.AWS.SecretAccessKey),
			Region:          ptr(plan.AWS.Region),
			Endpoint:        ptr(plan.AWS.Endpoint),
			MessageGroupID:  ptr(plan.AWS.MessageGroupID),
		}
	}
	var w client.WebhookEndpoint
	if err := r.client.Create(ctx, "/v1/webhooks", in, &w); err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the event destination", err, eventDestinationParams)
		return
	}
	secret := w.Secret
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), w.ID)...)
	if !plan.Enabled.IsNull() && !plan.Enabled.ValueBool() {
		if err := r.client.Patch(ctx, "/v1/webhooks/"+client.PathEscape(w.ID), map[string]any{"enabled": false}, &w); err != nil {
			addAPIError(&resp.Diagnostics, "Could not turn the new event destination off", err, eventDestinationParams)
			return
		}
	}
	state := plan
	state.fromAPI(&w, &plan)
	state.SigningSecret = strOrNull(secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *eventDestinationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state eventDestinationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w client.WebhookEndpoint
	err := r.client.Get(ctx, "/v1/webhooks/"+client.PathEscape(state.ID.ValueString()), nil, &w)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the event destination", err, nil)
		return
	}
	prior := state
	state.fromAPI(&w, &prior)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *eventDestinationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state eventDestinationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.WebhookInput{Description: ptr(plan.Description), Enabled: boolPtr(plan.Enabled)}
	in.Events = setStrings(ctx, plan.Events, &resp.Diagnostics)
	if !plan.URL.Equal(state.URL) {
		in.URL = ptr(plan.URL)
	}
	if !plan.TopicARN.Equal(state.TopicARN) {
		in.TopicARN = ptr(plan.TopicARN)
	}
	if !plan.QueueURL.Equal(state.QueueURL) {
		in.QueueURL = ptr(plan.QueueURL)
	}
	if plan.AWS != nil && (state.AWS == nil || *plan.AWS != *state.AWS) {
		in.AWS = &client.WebhookAWSInput{
			AccessKeyID:    ptr(plan.AWS.AccessKeyID),
			Region:         ptr(plan.AWS.Region),
			Endpoint:       ptr(plan.AWS.Endpoint),
			MessageGroupID: ptr(plan.AWS.MessageGroupID),
		}
		if state.AWS == nil || !plan.AWS.SecretAccessKey.Equal(state.AWS.SecretAccessKey) {
			in.AWS.SecretAccessKey = ptr(plan.AWS.SecretAccessKey)
		}
	}
	id := state.ID.ValueString()
	var w client.WebhookEndpoint
	if err := r.client.Patch(ctx, "/v1/webhooks/"+client.PathEscape(id), in, &w); err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the event destination", err, eventDestinationParams)
		return
	}
	secret := state.SigningSecret
	if !plan.SecretVersion.Equal(state.SecretVersion) {
		if err := r.client.Post(ctx, "/v1/webhooks/"+client.PathEscape(id)+"/rotate-secret", nil, &w); err != nil {
			addAPIError(&resp.Diagnostics, "Could not rotate the signing secret", err, nil)
			return
		}
		secret = strOrNull(w.Secret)
	}
	next := plan
	next.ID = state.ID
	next.fromAPI(&w, &plan)
	next.SigningSecret = secret
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *eventDestinationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state eventDestinationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, "/v1/webhooks/"+client.PathEscape(state.ID.ValueString())); err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the event destination", err, nil)
	}
}

func (r *eventDestinationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// fromAPI copies an endpoint from the API. The signing secret and aws.secret_access_key are not
// returned: the caller keeps them.
func (m *eventDestinationModel) fromAPI(w *client.WebhookEndpoint, prior *eventDestinationModel) {
	m.ID = types.StringValue(w.ID)
	m.Type = types.StringValue(w.Type)
	m.Target = types.StringValue(w.URL)
	m.ConnectionID = strOrNull(w.ConnectionID)
	m.URL, m.TopicARN, m.QueueURL = types.StringNull(), types.StringNull(), types.StringNull()
	if w.ConnectionID == nil {
		switch w.Type {
		case "sns":
			m.TopicARN = keepIfEquivalent(prior.TopicARN, firstNonNil(w.TopicARN, &w.URL), sameString)
		case "sqs":
			m.QueueURL = keepIfEquivalent(prior.QueueURL, firstNonNil(w.QueueURL, &w.URL), sameURL)
		default:
			m.URL = keepIfEquivalent(prior.URL, &w.URL, sameURL)
		}
	}
	if w.AWS != nil && w.ConnectionID == nil {
		var p awsModel
		if prior.AWS != nil {
			p = *prior.AWS
		} else {
			p = awsModel{SecretAccessKey: types.StringNull(), Region: types.StringNull(), Endpoint: types.StringNull(), MessageGroupID: types.StringNull()}
		}
		derived := setup.TopicRegion(w.URL)
		if w.Type == "sqs" {
			derived = setup.QueueRegion(w.URL)
		}
		region := keepIfEquivalent(p.Region, &w.AWS.Region, sameString)
		if p.Region.IsNull() && w.AWS.Region == derived {
			region = types.StringNull()
		}
		m.AWS = &awsModel{
			AccessKeyID:     types.StringValue(w.AWS.AccessKeyID),
			SecretAccessKey: p.SecretAccessKey,
			Region:          region,
			Endpoint:        keepIfEquivalent(p.Endpoint, w.AWS.Endpoint, sameURL),
			MessageGroupID:  keepIfEquivalent(p.MessageGroupID, w.AWS.MessageGroupID, sameString),
		}
	} else {
		m.AWS = nil
	}
	m.Events = stringSet(w.Events)
	m.Description = types.StringValue(w.Description)
	m.Enabled = types.BoolValue(w.Enabled)
	m.FailureCount = types.Int64Value(w.FailureCount)
	m.SecretVersion = prior.SecretVersion
	m.SigningSecret = prior.SigningSecret
	if m.SigningSecret.IsUnknown() {
		m.SigningSecret = types.StringNull()
	}
}

func firstNonNil(values ...*string) *string {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// requiresReplaceWhenRemoved replaces the resource when a value that was set is removed: the API
// keeps the stored value when an update leaves it out, and has no way to clear it.
func requiresReplaceWhenRemoved() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = known(req.StateValue) && req.ConfigValue.IsNull()
		},
		"Removing this value replaces the resource: the API cannot clear it in place.",
		"Removing this value replaces the resource: the API cannot clear it in place.",
	)
}
