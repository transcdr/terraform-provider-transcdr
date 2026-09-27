package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/transcdr/terraform-provider-transcdr/internal/client"
	"github.com/transcdr/terraform-provider-transcdr/internal/setup"
)

var (
	_ resource.Resource                = (*connectionResource)(nil)
	_ resource.ResourceWithConfigure   = (*connectionResource)(nil)
	_ resource.ResourceWithImportState = (*connectionResource)(nil)
)

// Storage kinds hold files; messaging kinds receive events (and an sqs queue can trigger automations).
var (
	storageKinds   = []string{"s3", "gcs", "azure_blob", "ftp", "ftps", "sftp", "http", "webdav"}
	messagingKinds = []string{"sqs", "sns", "webhook"}
)

func newConnectionResource() resource.Resource { return &connectionResource{} }

type connectionResource struct {
	client *client.Client
}

type connectionModel struct {
	ID             types.String           `tfsdk:"id"`
	Name           types.String           `tfsdk:"name"`
	Kind           types.String           `tfsdk:"kind"`
	Config         *connectionConfigModel `tfsdk:"config"`
	Secrets        *connectionSecrets     `tfsdk:"secrets"`
	Enabled        types.Bool             `tfsdk:"enabled"`
	Class          types.String           `tfsdk:"class"`
	Status         types.String           `tfsdk:"status"`
	Capabilities   types.Object           `tfsdk:"capabilities"`
	FailureCount   types.Int64            `tfsdk:"failure_count"`
	DisabledReason types.String           `tfsdk:"disabled_reason"`
	LastError      types.String           `tfsdk:"last_error"`
	SecretsSet     types.Set              `tfsdk:"secrets_set"`
}

type connectionConfigModel struct {
	Endpoint           types.String `tfsdk:"endpoint"`
	Bucket             types.String `tfsdk:"bucket"`
	Region             types.String `tfsdk:"region"`
	PathStyle          types.Bool   `tfsdk:"path_style"`
	Account            types.String `tfsdk:"account"`
	Host               types.String `tfsdk:"host"`
	Port               types.Int64  `tfsdk:"port"`
	Username           types.String `tfsdk:"username"`
	Root               types.String `tfsdk:"root"`
	Passive            types.Bool   `tfsdk:"passive"`
	HostKeyFingerprint types.String `tfsdk:"host_key_fingerprint"`
	QueueURL           types.String `tfsdk:"queue_url"`
	TopicARN           types.String `tfsdk:"topic_arn"`
	URL                types.String `tfsdk:"url"`
	MessageGroupID     types.String `tfsdk:"message_group_id"`
}

type connectionSecrets struct {
	AccessKeyID          types.String `tfsdk:"access_key_id"`
	SecretAccessKey      types.String `tfsdk:"secret_access_key"`
	SessionToken         types.String `tfsdk:"session_token"`
	Password             types.String `tfsdk:"password"`
	PrivateKey           types.String `tfsdk:"private_key"`
	PrivateKeyPassphrase types.String `tfsdk:"private_key_passphrase"`
	ServiceAccountJSON   types.String `tfsdk:"service_account_json"`
	AccountKey           types.String `tfsdk:"account_key"`
	SASToken             types.String `tfsdk:"sas_token"`
	BearerToken          types.String `tfsdk:"bearer_token"`
}

// secretFields pairs each secret's API name with its field in the model.
func (s *connectionSecrets) fields() []struct {
	name  string
	value *types.String
} {
	return []struct {
		name  string
		value *types.String
	}{
		{"access_key_id", &s.AccessKeyID},
		{"secret_access_key", &s.SecretAccessKey},
		{"session_token", &s.SessionToken},
		{"password", &s.Password},
		{"private_key", &s.PrivateKey},
		{"private_key_passphrase", &s.PrivateKeyPassphrase},
		{"service_account_json", &s.ServiceAccountJSON},
		{"account_key", &s.AccountKey},
		{"sas_token", &s.SASToken},
		{"bearer_token", &s.BearerToken},
	}
}

var capabilitiesType = map[string]attr.Type{
	"source":      types.BoolType,
	"destination": types.BoolType,
	"watch":       types.BoolType,
	"trigger":     types.BoolType,
	"events":      types.BoolType,
}

func (r *connectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func configAttributes() map[string]schema.Attribute {
	s := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc}
	}
	return map[string]schema.Attribute{
		"endpoint":             s("`s3`: a custom endpoint (R2, B2, Wasabi, MinIO…); omit for AWS. `http`/`webdav`: the base URL. `sns`: an SNS-compatible service endpoint instead of AWS's."),
		"bucket":               s("`s3`/`gcs`: the bucket. `azure_blob`: the container."),
		"region":               s("`s3`: the bucket's region (`auto` for R2). `sqs`/`sns`: read from the queue URL or topic ARN when omitted; required when it cannot be (e.g. a non-AWS queue URL)."),
		"path_style":           schema.BoolAttribute{Optional: true, MarkdownDescription: "`s3`: address the bucket by path rather than by subdomain (MinIO, LocalStack). Default false."},
		"account":              s("`azure_blob`: the storage account name."),
		"host":                 s("`ftp`/`ftps`/`sftp`: the host name."),
		"port":                 schema.Int64Attribute{Optional: true, MarkdownDescription: "`ftp`/`ftps`/`sftp`: the port, when not the protocol's default."},
		"username":             s("`ftp`/`ftps`/`sftp`/`webdav`: the login name; `http`: the basic-auth user name."),
		"root":                 s("Every path is relative to this folder inside the connection, e.g. `videos/`."),
		"passive":              schema.BoolAttribute{Optional: true, MarkdownDescription: "`ftp`/`ftps`: passive mode (the API's default is true)."},
		"host_key_fingerprint": s("`sftp`: the expected host key, `SHA256:…`. Any other key is refused."),
		"queue_url":            s("`sqs`: the queue URL, e.g. `https://sqs.us-east-1.amazonaws.com/123456789012/transcdr`."),
		"topic_arn":            s("`sns`: the topic ARN."),
		"url":                  s("`webhook`: the https URL events are POSTed to."),
		"message_group_id":     s("`sqs`/`sns`: the message group of FIFO queues and topics receiving events."),
	}
}

func secretAttributes() map[string]schema.Attribute {
	s := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: desc}
	}
	return map[string]schema.Attribute{
		"access_key_id":          s("`s3`, `sqs`, `sns`: the access key ID."),
		"secret_access_key":      s("`s3`, `sqs`, `sns`: the secret access key."),
		"session_token":          s("`s3`: a session token, for temporary credentials."),
		"password":               s("`ftp`/`ftps`/`sftp`/`webdav`, and `http` basic auth (with `config.username`)."),
		"private_key":            s("`sftp`: a private key (OpenSSH or PEM)."),
		"private_key_passphrase": s("`sftp`: the private key's passphrase."),
		"service_account_json":   s("`gcs`: a service account JSON key."),
		"account_key":            s("`azure_blob`: the account key (or use `sas_token`)."),
		"sas_token":              s("`azure_blob`: a SAS token."),
		"bearer_token":           s("`http`/`webdav`: sent as `Authorization: Bearer`."),
	}
}

func (r *connectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A connection: **storage** (`s3`, `gcs`, `azure_blob`, `ftp`, `ftps`, `sftp`, `http`, `webdav`) where inputs come from and outputs go, " +
			"or **messaging** (`sqs`, `sns`, `webhook`) that receives events; an `sqs` connection can also trigger queue automations. Needs the Starter plan or above.\n\n" +
			"Transcdr tries the credentials when the connection is saved and records the outcome in `status` and `last_error`.\n\n" +
			"**Secrets are write-only.** The API never returns them, so Terraform keeps the values from your configuration, and cannot see a secret changed outside Terraform. " +
			"To rotate a credential, change its value here and apply. Removing a secret from the configuration clears it. " +
			"If a secret Terraform set is no longer stored (see `secrets_set`), the next plan puts it back.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The connection id, `con_…`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name, up to 120 characters.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 120)},
			},
			"kind": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "One of `s3`, `gcs`, `azure_blob`, `ftp`, `ftps`, `sftp`, `http`, `webdav` (storage) or `sqs`, `sns`, `webhook` (messaging). Changing it replaces the connection.",
				Validators:          []validator.String{stringvalidator.OneOf(append(slices.Clone(storageKinds), messagingKinds...)...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"config": schema.SingleNestedAttribute{
				Required: true,
				MarkdownDescription: "Non-secret settings. Which fields apply depends on `kind`:\n\n" +
					"| kind | config |\n|---|---|\n" +
					"| `s3` (AWS, R2, B2, Wasabi, MinIO…) | `bucket`, `region`, `endpoint`, `path_style`, `root` |\n" +
					"| `gcs` | `bucket`, `root` |\n" +
					"| `azure_blob` | `account`, `bucket` (the container), `root` |\n" +
					"| `ftp` / `ftps` | `host`, `port`, `username`, `root`, `passive` |\n" +
					"| `sftp` | `host`, `port`, `username`, `root`, `host_key_fingerprint` |\n" +
					"| `http` (read-only) | `endpoint`, `username` |\n" +
					"| `webdav` | `endpoint`, `username`, `root` |\n" +
					"| `sqs` | `queue_url`, `region`, `message_group_id` |\n" +
					"| `sns` | `topic_arn`, `region`, `endpoint`, `message_group_id` |\n" +
					"| `webhook` | `url` |\n\n" +
					"Updates merge into the stored config; a field removed here is cleared.",
				Attributes: configAttributes(),
			},
			"secrets": schema.SingleNestedAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Write-only credentials:\n\n" +
					"| kind | secrets |\n|---|---|\n" +
					"| `s3` | `access_key_id`, `secret_access_key`, `session_token` |\n" +
					"| `gcs` | `service_account_json` |\n" +
					"| `azure_blob` | `account_key` or `sas_token` |\n" +
					"| `ftp` / `ftps` | `password` |\n" +
					"| `sftp` | `password` or `private_key` (+ `private_key_passphrase`) |\n" +
					"| `http` | `password` (basic, with `config.username`) or `bearer_token` |\n" +
					"| `webdav` | `password` or `bearer_token` |\n" +
					"| `sqs` / `sns` | `access_key_id`, `secret_access_key` |\n" +
					"| `webhook` | none: events are signed with the event destination's secret |\n\n" +
					"Never read back from the API: see the note above on rotation.",
				Attributes: secretAttributes(),
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether the connection is on. Transcdr turns a connection off by itself after a permanent failure (or 5 transient ones in a row); " +
					"the next plan then shows `enabled` going back to `true`, and applying it tests the connection again and turns it back on. Default true.",
			},
			"class": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`storage` or `messaging`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The last test's outcome: `untested`, `ok` or `error`.",
			},
			"capabilities": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "What the connection can be used for.",
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"source":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Inputs can be read from it."},
					"destination": schema.BoolAttribute{Computed: true, MarkdownDescription: "Outputs can be delivered to it."},
					"watch":       schema.BoolAttribute{Computed: true, MarkdownDescription: "It can be listed, so a `watch` automation can poll it."},
					"trigger":     schema.BoolAttribute{Computed: true, MarkdownDescription: "It can trigger `queue` automations (`sqs`)."},
					"events":      schema.BoolAttribute{Computed: true, MarkdownDescription: "It can receive events (messaging kinds)."},
				},
			},
			"failure_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Transient failures in a row; any success resets it.",
			},
			"disabled_reason": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Why the connection was turned off: `<activity>: <error>`, or `Disabled by hand.`.",
			},
			"last_error": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The last test's error, if it failed.",
			},
			"secrets_set": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The names of the secrets that are stored. Their values are never returned.",
			},
		},
	}
}

func (r *connectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

var connectionParams = topLevelParam(map[string][]string{
	"name": nil, "kind": nil,
	"config":  {"endpoint", "bucket", "region", "path_style", "account", "host", "port", "username", "root", "passive", "host_key_fingerprint", "queue_url", "topic_arn", "url", "message_group_id"},
	"secrets": {"access_key_id", "secret_access_key", "session_token", "password", "private_key", "private_key_passphrase", "service_account_json", "account_key", "sas_token", "bearer_token"},
})

// configBody is the config as sent. With prior (an update), fields set before and now removed are
// sent as null (false for path_style) so the API's merge clears them.
func configBody(cfg, prior *connectionConfigModel) map[string]any {
	body := map[string]any{}
	put := func(name string, now, before attr.Value, value any, cleared any) {
		switch {
		case known(now):
			body[name] = value
		case prior != nil && known(before):
			body[name] = cleared
		}
	}
	var p connectionConfigModel
	if prior != nil {
		p = *prior
	}
	put("endpoint", cfg.Endpoint, p.Endpoint, cfg.Endpoint.ValueString(), nil)
	put("bucket", cfg.Bucket, p.Bucket, cfg.Bucket.ValueString(), nil)
	put("region", cfg.Region, p.Region, cfg.Region.ValueString(), nil)
	put("path_style", cfg.PathStyle, p.PathStyle, cfg.PathStyle.ValueBool(), false)
	put("account", cfg.Account, p.Account, cfg.Account.ValueString(), nil)
	put("host", cfg.Host, p.Host, cfg.Host.ValueString(), nil)
	put("port", cfg.Port, p.Port, cfg.Port.ValueInt64(), nil)
	put("username", cfg.Username, p.Username, cfg.Username.ValueString(), nil)
	put("root", cfg.Root, p.Root, cfg.Root.ValueString(), nil)
	put("passive", cfg.Passive, p.Passive, cfg.Passive.ValueBool(), nil)
	put("host_key_fingerprint", cfg.HostKeyFingerprint, p.HostKeyFingerprint, cfg.HostKeyFingerprint.ValueString(), nil)
	put("queue_url", cfg.QueueURL, p.QueueURL, cfg.QueueURL.ValueString(), nil)
	put("topic_arn", cfg.TopicARN, p.TopicARN, cfg.TopicARN.ValueString(), nil)
	put("url", cfg.URL, p.URL, cfg.URL.ValueString(), nil)
	put("message_group_id", cfg.MessageGroupID, p.MessageGroupID, cfg.MessageGroupID.ValueString(), nil)
	return body
}

// secretsBody is the secrets as sent: on create every one set; on update only those that changed,
// with `""` for one removed (which clears it).
func secretsBody(now, prior *connectionSecrets) map[string]string {
	body := map[string]string{}
	var n, p connectionSecrets
	if now != nil {
		n = *now
	}
	if prior != nil {
		p = *prior
	}
	nf, pf := n.fields(), p.fields()
	for i := range nf {
		nv, pv := *nf[i].value, *pf[i].value
		switch {
		case known(nv) && (prior == nil || !nv.Equal(pv)):
			body[nf[i].name] = nv.ValueString()
		case prior != nil && known(pv) && nv.IsNull():
			body[nf[i].name] = ""
		}
	}
	return body
}

func (r *connectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan connectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"name":   plan.Name.ValueString(),
		"kind":   plan.Kind.ValueString(),
		"config": configBody(plan.Config, nil),
	}
	if s := secretsBody(plan.Secrets, nil); len(s) > 0 {
		body["secrets"] = s
	}
	var conn client.Connection
	if err := r.client.Create(ctx, "/v1/connections", body, &conn); err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the connection", err, connectionParams)
		return
	}
	// Save the id first so a failure below leaves the connection in state, not orphaned.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), conn.ID)...)
	if !plan.Enabled.IsNull() && !plan.Enabled.ValueBool() {
		if err := r.client.Patch(ctx, "/v1/connections/"+client.PathEscape(conn.ID), map[string]any{"enabled": false}, &conn); err != nil {
			addAPIError(&resp.Diagnostics, "Could not turn the new connection off", err, connectionParams)
			return
		}
	}
	state := plan
	resp.Diagnostics.Append(state.fromAPI(&conn, &plan, true)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state connectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var conn client.Connection
	err := r.client.Get(ctx, "/v1/connections/"+client.PathEscape(state.ID.ValueString()), nil, &conn)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the connection", err, nil)
		return
	}
	prior := state
	resp.Diagnostics.Append(state.fromAPI(&conn, &prior, false)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state connectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{}
	if !plan.Name.Equal(state.Name) {
		body["name"] = plan.Name.ValueString()
	}
	if !plan.Enabled.Equal(state.Enabled) {
		body["enabled"] = plan.Enabled.ValueBool()
	}
	if cfg := configBody(plan.Config, state.Config); !sameConfig(plan.Config, state.Config) {
		body["config"] = cfg
	}
	if s := secretsBody(plan.Secrets, orEmpty(state.Secrets)); len(s) > 0 {
		body["secrets"] = s
	}
	var conn client.Connection
	id := state.ID.ValueString()
	if len(body) == 0 {
		if err := r.client.Get(ctx, "/v1/connections/"+client.PathEscape(id), nil, &conn); err != nil {
			addAPIError(&resp.Diagnostics, "Could not read the connection", err, nil)
			return
		}
	} else if err := r.client.Patch(ctx, "/v1/connections/"+client.PathEscape(id), body, &conn); err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the connection", err, connectionParams)
		return
	}
	next := plan
	next.ID = state.ID
	resp.Diagnostics.Append(next.fromAPI(&conn, &plan, true)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func orEmpty(s *connectionSecrets) *connectionSecrets {
	if s == nil {
		return &connectionSecrets{}
	}
	return s
}

func sameConfig(a, b *connectionConfigModel) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (r *connectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state connectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Delete(ctx, "/v1/connections/"+client.PathEscape(state.ID.ValueString()))
	if client.IsConflict(err) {
		resp.Diagnostics.AddError("The connection is still in use",
			"Transcdr refuses to delete a connection while an automation uses it as its source or destination. "+
				"Delete those automations, or point them at another connection, first. In Terraform, reference the connection's id from the "+
				"transcdr_automation (rather than a literal) so the automation is destroyed or updated before the connection.\n\n"+err.(interface{ Detail() string }).Detail())
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the connection", err, nil)
	}
}

func (r *connectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// fromAPI copies an API connection into the model. prior is the configuration's view (the plan
// after a write, the state on refresh): fields that mean the same as the API's value keep their
// spelling, server-derived values the configuration left out stay null, and secrets stay as
// configured, since they are never returned.
func (m *connectionModel) fromAPI(conn *client.Connection, prior *connectionModel, afterWrite bool) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(conn.ID)
	m.Name = types.StringValue(conn.Name)
	m.Kind = types.StringValue(conn.Kind)
	m.Enabled = types.BoolValue(conn.Enabled)
	m.Class = types.StringValue(conn.Class)
	m.Status = types.StringValue(conn.Status)
	m.FailureCount = types.Int64Value(conn.FailureCount)
	m.DisabledReason = strOrNull(conn.DisabledReason)
	m.LastError = strOrNull(conn.LastError)
	m.SecretsSet = stringSet(conn.SecretsSet)
	caps, d := types.ObjectValue(capabilitiesType, map[string]attr.Value{
		"source":      types.BoolValue(conn.Capabilities.Source),
		"destination": types.BoolValue(conn.Capabilities.Destination),
		"watch":       types.BoolValue(conn.Capabilities.Watch),
		"trigger":     types.BoolValue(conn.Capabilities.Trigger),
		"events":      types.BoolValue(conn.Capabilities.Events),
	})
	diags.Append(d...)
	m.Capabilities = caps

	if afterWrite && prior != nil && prior.Config != nil {
		// Right after a write the configuration is what was sent; a refresh shows any difference.
		cfg := *prior.Config
		m.Config = &cfg
	} else {
		m.Config = configFromAPI(conn, prior)
	}
	m.Secrets = secretsAfterRead(conn, prior, afterWrite)
	return diags
}

func configFromAPI(conn *client.Connection, prior *connectionModel) *connectionConfigModel {
	p := nullConfig()
	if prior != nil && prior.Config != nil {
		p = *prior.Config
	}
	c := conn.Config
	derivedRegion := ""
	switch conn.Kind {
	case "sqs":
		if c.QueueURL != nil {
			derivedRegion = setup.QueueRegion(*c.QueueURL)
		}
	case "sns":
		if c.TopicARN != nil {
			derivedRegion = setup.TopicRegion(*c.TopicARN)
		}
	}
	cfg := connectionConfigModel{
		Endpoint:           keepIfEquivalent(p.Endpoint, c.Endpoint, sameURL),
		Bucket:             keepIfEquivalent(p.Bucket, c.Bucket, sameString),
		Region:             keepIfEquivalent(p.Region, c.Region, sameString),
		Account:            keepIfEquivalent(p.Account, c.Account, sameString),
		Host:               keepIfEquivalent(p.Host, c.Host, sameString),
		Username:           keepIfEquivalent(p.Username, c.Username, sameString),
		Root:               keepIfEquivalent(p.Root, c.Root, samePath),
		HostKeyFingerprint: keepIfEquivalent(p.HostKeyFingerprint, c.HostKeyFingerprint, sameString),
		QueueURL:           keepIfEquivalent(p.QueueURL, c.QueueURL, sameURL),
		TopicARN:           keepIfEquivalent(p.TopicARN, c.TopicARN, sameString),
		URL:                keepIfEquivalent(p.URL, c.URL, sameURL),
		MessageGroupID:     keepIfEquivalent(p.MessageGroupID, c.MessageGroupID, sameString),
		PathStyle:          types.BoolNull(),
		Port:               types.Int64Null(),
		Passive:            types.BoolNull(),
	}
	// A region the API read from the queue URL or topic ARN is not a setting of the configuration.
	if p.Region.IsNull() && c.Region != nil && *c.Region == derivedRegion {
		cfg.Region = types.StringNull()
	}
	// The API always returns path_style; false is its default.
	if c.PathStyle != nil && (*c.PathStyle || !p.PathStyle.IsNull()) {
		cfg.PathStyle = types.BoolValue(*c.PathStyle)
	}
	if c.Port != nil {
		cfg.Port = types.Int64Value(*c.Port)
	}
	if c.Passive != nil {
		cfg.Passive = types.BoolValue(*c.Passive)
	}
	return &cfg
}

// secretsAfterRead keeps the secrets as configured, since the API never returns them. On a refresh,
// one that was stored and no longer is (per secrets_set) is dropped, so the next plan sends it again.
func secretsAfterRead(conn *client.Connection, prior *connectionModel, afterWrite bool) *connectionSecrets {
	if prior == nil || prior.Secrets == nil {
		return nil
	}
	secrets := *prior.Secrets
	if afterWrite {
		return &secrets
	}
	stored := map[string]bool{}
	for _, name := range conn.SecretsSet {
		stored[name] = true
	}
	wasStored := map[string]bool{}
	if known(prior.SecretsSet) {
		for _, v := range prior.SecretsSet.Elements() {
			if s, ok := v.(basetypes.StringValue); ok {
				wasStored[s.ValueString()] = true
			}
		}
	}
	for _, f := range secrets.fields() {
		if known(*f.value) && wasStored[f.name] && !stored[f.name] {
			*f.value = types.StringNull()
		}
	}
	return &secrets
}

func nullConfig() connectionConfigModel {
	return connectionConfigModel{
		Endpoint: types.StringNull(), Bucket: types.StringNull(), Region: types.StringNull(), PathStyle: types.BoolNull(),
		Account: types.StringNull(), Host: types.StringNull(), Port: types.Int64Null(), Username: types.StringNull(),
		Root: types.StringNull(), Passive: types.BoolNull(), HostKeyFingerprint: types.StringNull(), QueueURL: types.StringNull(),
		TopicARN: types.StringNull(), URL: types.StringNull(), MessageGroupID: types.StringNull(),
	}
}
