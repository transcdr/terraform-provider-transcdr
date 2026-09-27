package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

// ---- transcdr_organization -------------------------------------------------------------

func newOrganizationDataSource() datasource.DataSource { return &organizationDataSource{} }

type organizationDataSource struct {
	client *transcdr.Client
}

type organizationModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Slug              types.String `tfsdk:"slug"`
	Plan              types.String `tfsdk:"plan"`
	PlanName          types.String `tfsdk:"plan_name"`
	Features          types.List   `tfsdk:"features"`
	MaxResolution     types.Int64  `tfsdk:"max_resolution"`
	MaxConcurrentJobs types.Int64  `tfsdk:"max_concurrent_jobs"`
	Priority          types.Bool   `tfsdk:"priority"`
	RetentionDays     types.Int64  `tfsdk:"retention_days"`
	BillingEmail      types.String `tfsdk:"billing_email"`
	Suspended         types.Bool   `tfsdk:"suspended"`
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	c := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The organization the API key belongs to: its plan and the plan's features, e.g. to check for `integrations` or `webhooks` before creating connections or event destinations. Needs `org:read`.",
		Attributes: map[string]schema.Attribute{
			"id":                  c("The organization id, `org_…`."),
			"name":                c("The organization's name."),
			"slug":                c("The organization's slug."),
			"plan":                c("The plan id, e.g. `starter`."),
			"plan_name":           c("The plan's display name."),
			"features":            schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The plan's features, e.g. `hls`, `webhooks`, `api`, `integrations`."},
			"max_resolution":      schema.Int64Attribute{Computed: true, MarkdownDescription: "The largest short side a rendition may have on this plan."},
			"max_concurrent_jobs": schema.Int64Attribute{Computed: true, MarkdownDescription: "Jobs that may run at once."},
			"priority":            schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether `priority = \"high\"` is allowed."},
			"retention_days":      schema.Int64Attribute{Computed: true, MarkdownDescription: "How long outputs are kept."},
			"billing_email":       c("The billing contact."),
			"suspended":           schema.BoolAttribute{Computed: true, MarkdownDescription: "Suspended organizations cannot create jobs."},
		},
	}
}

func (d *organizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *organizationDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	org, err := d.client.Organization.Get(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the organization", err, nil)
		return
	}
	m := organizationModel{
		ID:           types.StringValue(org.ID),
		Name:         types.StringValue(org.Name),
		Slug:         types.StringValue(org.Slug),
		Plan:         types.StringValue(org.Plan),
		BillingEmail: strOrNull(org.BillingEmail),
		Suspended:    types.BoolValue(org.Suspended != nil && *org.Suspended),
		PlanName:     types.StringNull(),
		Features:     stringList(nil),
	}
	m.MaxResolution, m.MaxConcurrentJobs, m.RetentionDays = types.Int64Null(), types.Int64Null(), types.Int64Null()
	m.Priority = types.BoolNull()
	if p := org.PlanDetails; p != nil {
		m.PlanName = types.StringValue(p.Name)
		m.Features = stringList(p.Features)
		m.MaxResolution = types.Int64Value(int64(p.MaxResolution))
		m.MaxConcurrentJobs = types.Int64Value(int64(p.MaxConcurrentJobs))
		m.Priority = types.BoolValue(p.Priority)
		m.RetentionDays = types.Int64Value(int64(p.RetentionDays))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---- transcdr_preset -------------------------------------------------------------------

func newPresetDataSource() datasource.DataSource { return &presetDataSource{} }

type presetDataSource struct {
	client *transcdr.Client
}

type presetDataModel struct {
	ID          types.String `tfsdk:"id"`
	Slug        types.String `tfsdk:"slug"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	System      types.Bool   `tfsdk:"system"`
	Output      types.String `tfsdk:"output"`
	Metadata    types.Map    `tfsdk:"metadata"`
}

func (d *presetDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_preset"
}

func (d *presetDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A preset by slug or id: a system preset (`web-av1-1080p`, `web-av1-720p`, `hls-av1-abr`, `hls-h264-abr`, `mp4-h264-compat-1080p`, " +
			"`mp4-h265-1080p`, `hdr10-av1-2160p`, `social-vertical-1080x1920`, `audio-strip-av1-720p`, `archive-av1-high`) or one of the organization's. Needs `presets:read`.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "`pre_…`, or the slug for a system preset. Give this or `slug`."},
			"slug":        schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "The preset's slug. Give this or `id`."},
			"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "The preset's name."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "The preset's description."},
			"system":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is a built-in system preset."},
			"output":      schema.StringAttribute{Computed: true, MarkdownDescription: "The full output specification, as JSON."},
			"metadata":    schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The preset's metadata."},
		},
	}
}

func (d *presetDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug"))}
}

func (d *presetDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *presetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m presetDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ref := m.ID.ValueString()
	if ref == "" {
		ref = m.Slug.ValueString()
	}
	p, err := d.client.Presets.Get(ctx, ref)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the preset "+ref, err, nil)
		return
	}
	m.ID = types.StringValue(p.ID)
	m.Slug = types.StringValue(p.Slug)
	m.Name = types.StringValue(p.Name)
	m.Description = types.StringValue(p.Description)
	m.System = types.BoolValue(p.System)
	m.Output = types.StringValue(compactJSON(p.Output.Raw()))
	m.Metadata = metadataValue(types.MapValueMust(types.StringType, map[string]attr.Value{}), p.Metadata)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---- transcdr_connection ---------------------------------------------------------------

func newConnectionDataSource() datasource.DataSource { return &connectionDataSource{} }

type connectionDataSource struct {
	client *transcdr.Client
}

type connectionDataModel struct {
	ID             types.String           `tfsdk:"id"`
	Name           types.String           `tfsdk:"name"`
	Kind           types.String           `tfsdk:"kind"`
	Config         *connectionConfigModel `tfsdk:"config"`
	Enabled        types.Bool             `tfsdk:"enabled"`
	Class          types.String           `tfsdk:"class"`
	Status         types.String           `tfsdk:"status"`
	Capabilities   types.Object           `tfsdk:"capabilities"`
	FailureCount   types.Int64            `tfsdk:"failure_count"`
	DisabledReason types.String           `tfsdk:"disabled_reason"`
	LastError      types.String           `tfsdk:"last_error"`
	SecretsSet     types.Set              `tfsdk:"secrets_set"`
}

func (d *connectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (d *connectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	cfg := map[string]schema.Attribute{}
	for name := range configAttributes() {
		switch name {
		case "path_style", "passive":
			cfg[name] = schema.BoolAttribute{Computed: true}
		case "port":
			cfg[name] = schema.Int64Attribute{Computed: true}
		default:
			cfg[name] = schema.StringAttribute{Computed: true}
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A connection by id or by name, e.g. one made in the dashboard, to use as an automation's source, destination or trigger. Secrets are never returned. Needs `connections:read`.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "`con_…`. Give this or `name`."},
			"name":            schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "The connection's name. Give this or `id`; it must match exactly one connection."},
			"kind":            schema.StringAttribute{Computed: true, MarkdownDescription: "The connection's kind, e.g. `s3` or `sqs`."},
			"config":          schema.SingleNestedAttribute{Computed: true, Attributes: cfg, MarkdownDescription: "The non-secret settings (see the `transcdr_connection` resource)."},
			"enabled":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the connection is on."},
			"class":           schema.StringAttribute{Computed: true, MarkdownDescription: "`storage` or `messaging`."},
			"status":          schema.StringAttribute{Computed: true, MarkdownDescription: "`untested`, `ok` or `error`."},
			"capabilities":    schema.ObjectAttribute{Computed: true, AttributeTypes: capabilitiesType, MarkdownDescription: "`source`, `destination`, `watch`, `trigger`, `events`."},
			"failure_count":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Transient failures in a row."},
			"disabled_reason": schema.StringAttribute{Computed: true, MarkdownDescription: "Why it was turned off."},
			"last_error":      schema.StringAttribute{Computed: true, MarkdownDescription: "The last test's error."},
			"secrets_set":     schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The names of the stored secrets."},
		},
	}
}

func (d *connectionDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name"))}
}

func (d *connectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *connectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg connectionDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var conn *transcdr.Connection
	if known(cfg.ID) {
		c, err := d.client.Connections.Get(ctx, cfg.ID.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Could not read the connection", err, nil)
			return
		}
		conn = c
	} else {
		all, err := transcdr.Collect(d.client.Connections.All(ctx, &transcdr.ListParams{Limit: 100}), 0)
		if err != nil {
			addAPIError(&resp.Diagnostics, "Could not list the connections", err, nil)
			return
		}
		name := cfg.Name.ValueString()
		for i := range all {
			if all[i].Name == name {
				if conn != nil {
					resp.Diagnostics.AddAttributeError(path.Root("name"), "Ambiguous connection name",
						"More than one connection is named "+name+" ("+conn.ID+", "+all[i].ID+"). Look it up by id instead.")
					return
				}
				conn = &all[i]
			}
		}
		if conn == nil {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "No such connection", "No connection is named "+name+".")
			return
		}
	}
	var m connectionModel
	resp.Diagnostics.Append(m.fromAPI(conn, nil, false)...)
	full := configFromAPI(conn, nil)
	out := connectionDataModel{
		ID: m.ID, Name: m.Name, Kind: m.Kind, Config: full, Enabled: m.Enabled, Class: m.Class, Status: m.Status,
		Capabilities: m.Capabilities, FailureCount: m.FailureCount, DisabledReason: m.DisabledReason, LastError: m.LastError, SecretsSet: m.SecretsSet,
	}
	// A data source shows every value, including the ones the API derived.
	if r := np(conn.Config.Region); r != nil {
		out.Config.Region = types.StringValue(*r)
	}
	if conn.Config.PathStyle != nil {
		out.Config.PathStyle = types.BoolValue(*conn.Config.PathStyle)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}
