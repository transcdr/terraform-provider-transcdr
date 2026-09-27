package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/transcdr/terraform-provider-transcdr/internal/client"
)

var (
	_ resource.Resource                = (*apiKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*apiKeyResource)(nil)
	_ resource.ResourceWithImportState = (*apiKeyResource)(nil)
)

// scopes are the scopes a key can carry, besides "*".
var scopes = []string{
	"jobs:read", "jobs:write", "assets:read", "assets:write", "presets:read", "presets:write",
	"webhooks:read", "webhooks:write", "usage:read", "billing:read", "billing:write", "keys:read", "keys:write",
	"org:read", "org:write", "connections:read", "connections:write", "automations:read", "automations:write",
}

func newAPIKeyResource() resource.Resource { return &apiKeyResource{} }

type apiKeyResource struct {
	client *client.Client
}

type apiKeyModel struct {
	ID         types.String      `tfsdk:"id"`
	Name       types.String      `tfsdk:"name"`
	Scopes     types.Set         `tfsdk:"scopes"`
	Mode       types.String      `tfsdk:"mode"`
	ExpiresAt  timetypes.RFC3339 `tfsdk:"expires_at"`
	Secret     types.String      `tfsdk:"secret"`
	Prefix     types.String      `tfsdk:"prefix"`
	CreatedAt  types.String      `tfsdk:"created_at"`
	LastUsedAt types.String      `tfsdk:"last_used_at"`
}

func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A secret API key, for example a narrowly scoped key for a CI job. The key that manages it needs `keys:write` " +
			"and can grant only scopes it holds itself. Keys cannot be changed: any change replaces the key (a new secret), and destroying it revokes it at once.\n\n" +
			"`secret` is returned only when the key is created. It is stored in the Terraform state, so protect the state accordingly; an imported key has no `secret`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The key id, `key_…`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name, up to 80 characters.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 80)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scopes": schema.SetAttribute{
				Optional: true, Computed: true,
				ElementType:         types.StringType,
				Default:             setdefault.StaticValue(stringSet([]string{"*"})),
				MarkdownDescription: "What the key may do: `*` (everything, the default) or any of `" + joinTicks(scopes) + "`.",
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.ValueStringsAre(stringvalidator.OneOf(append([]string{"*"}, scopes...)...))},
				PlanModifiers:       []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"mode": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("live"),
				MarkdownDescription: "`live` or `test`. Test-mode keys run jobs that complete with synthetic outputs after a few seconds and are free. Default `live`.",
				Validators:          []validator.String{stringvalidator.OneOf("live", "test")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"expires_at": schema.StringAttribute{
				CustomType:          timetypes.RFC3339Type{},
				Optional:            true,
				MarkdownDescription: "When the key stops working, an RFC 3339 time in the future (e.g. `2027-01-01T00:00:00Z`). Omit for a key that does not expire.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"secret": schema.StringAttribute{
				Computed: true, Sensitive: true,
				MarkdownDescription: "The key itself, `tdk_live_…` or `tdk_test_…`. Only available from the apply that created the key.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"prefix": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The key's visible start, e.g. `tdk_live_ab12`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_used_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key was last used.",
			},
		},
	}
}

func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

var apiKeyParams = topLevelParam(map[string][]string{"name": nil, "scopes": nil, "mode": nil, "expires_at": nil})

func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"name":   plan.Name.ValueString(),
		"scopes": setStrings(ctx, plan.Scopes, &resp.Diagnostics),
		"mode":   plan.Mode.ValueString(),
	}
	if known(plan.ExpiresAt) {
		body["expires_at"] = plan.ExpiresAt.ValueString()
	}
	var k client.APIKey
	if err := r.client.Create(ctx, "/v1/api-keys", body, &k); err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the API key", err, apiKeyParams)
		return
	}
	state := plan
	state.fromAPI(&k, &plan)
	state.Secret = strOrNull(k.Secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// findAPIKey looks a key up in the list: the API has no endpoint for one key. Revoked keys are
// not listed, so a revoked key is not found.
func findAPIKey(ctx context.Context, c *client.Client, id string) (*client.APIKey, error) {
	keys, err := client.ListAll(ctx, c, "/v1/api-keys", url.Values{}, func(k client.APIKey) bool { return k.ID == id })
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, nil
}

func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k, err := findAPIKey(ctx, r.client, state.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the API keys", err, nil)
		return
	}
	if k == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	prior := state
	state.fromAPI(k, &prior)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only records the plan: every argument forces a new key.
func (r *apiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan apiKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, "/v1/api-keys/"+client.PathEscape(state.ID.ValueString())); err != nil {
		addAPIError(&resp.Diagnostics, "Could not revoke the API key", err, nil)
	}
}

func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (m *apiKeyModel) fromAPI(k *client.APIKey, prior *apiKeyModel) {
	m.ID = types.StringValue(k.ID)
	m.Name = types.StringValue(k.Name)
	m.Scopes = stringSet(k.Scopes)
	m.Mode = types.StringValue(k.Mode)
	m.Prefix = types.StringValue(k.Prefix)
	m.CreatedAt = types.StringValue(k.CreatedAt)
	m.LastUsedAt = strOrNull(k.LastUsedAt)
	switch {
	case k.ExpiresAt == nil:
		m.ExpiresAt = timetypes.NewRFC3339Null()
	case known(prior.ExpiresAt) && sameInstant(prior.ExpiresAt, *k.ExpiresAt):
		m.ExpiresAt = prior.ExpiresAt
	default:
		m.ExpiresAt = timetypes.NewRFC3339ValueMust(*k.ExpiresAt)
	}
	m.Secret = prior.Secret
	if m.Secret.IsUnknown() {
		m.Secret = types.StringNull()
	}
}

func sameInstant(a timetypes.RFC3339, b string) bool {
	ta, d1 := a.ValueRFC3339Time()
	tb, d2 := timetypes.NewRFC3339ValueMust(b).ValueRFC3339Time()
	return !d1.HasError() && !d2.HasError() && ta.Equal(tb)
}
