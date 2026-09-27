// Package provider implements the Transcdr Terraform provider with the Terraform Plugin Framework.
package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

var _ provider.Provider = (*transcdrProvider)(nil)

type transcdrProvider struct {
	version string
}

type providerModel struct {
	APIKey     types.String `tfsdk:"api_key"`
	BaseURL    types.String `tfsdk:"base_url"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

// New returns the provider factory for a build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &transcdrProvider{version: version}
	}
}

func (p *transcdrProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "transcdr"
	resp.Version = p.version
}

func (p *transcdrProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Declare Transcdr storage and messaging connections, bucket automations, event destinations, presets and API keys as code.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "A secret API key (`tdk_live_…` or `tdk_test_…`). Defaults to the `TRANSCDR_API_KEY` environment variable. " +
					"The key needs the scopes of what you manage, e.g. `connections:write`, `automations:write`, `webhooks:write`, `presets:write`, `keys:write` and `org:read`.",
				Optional:  true,
				Sensitive: true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "The API base URL. Defaults to the `TRANSCDR_BASE_URL` environment variable, else `" + transcdr.DefaultBaseURL + "`.",
				Optional:            true,
			},
			"max_retries": schema.Int64Attribute{
				MarkdownDescription: "How many times a request that is safe to repeat (a read, a delete, or a PUT) is retried with exponential backoff after a 429, a 5xx or a network error. Default 4.",
				Optional:            true,
			},
		},
	}
}

func (p *transcdrProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.APIKey.IsUnknown() || config.BaseURL.IsUnknown() {
		// Known only after apply; resources are configured again then.
		return
	}

	apiKey := os.Getenv("TRANSCDR_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}
	baseURL := os.Getenv("TRANSCDR_BASE_URL")
	if !config.BaseURL.IsNull() {
		baseURL = config.BaseURL.ValueString()
	}
	if baseURL == "" {
		baseURL = transcdr.DefaultBaseURL
	}

	opts := []transcdr.Option{
		transcdr.WithAPIKey(apiKey),
		transcdr.WithBaseURL(baseURL),
		transcdr.WithUserAgent("terraform-provider-transcdr/" + p.version),
		transcdr.WithMaxRetries(4),
	}
	if !config.MaxRetries.IsNull() && !config.MaxRetries.IsUnknown() {
		n := config.MaxRetries.ValueInt64()
		if n < 0 {
			resp.Diagnostics.AddAttributeError(path.Root("max_retries"), "Invalid max_retries", "max_retries must be 0 or more.")
			return
		}
		opts = append(opts, transcdr.WithMaxRetries(int(n)))
	} else if v := os.Getenv("TRANSCDR_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts = append(opts, transcdr.WithMaxRetries(n))
		}
	}
	c := transcdr.NewClient(opts...)

	data := &providerData{client: c, hasKey: apiKey != ""}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *transcdrProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newConnectionResource,
		newAutomationResource,
		newEventDestinationResource,
		newPresetResource,
		newAPIKeyResource,
	}
}

func (p *transcdrProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newOrganizationDataSource,
		newPresetDataSource,
		newConnectionDataSource,
		newBucketAutomationSetupDataSource,
	}
}

// providerData is what Configure hands to resources and data sources.
type providerData struct {
	client *transcdr.Client
	hasKey bool
}
