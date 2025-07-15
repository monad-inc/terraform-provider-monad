package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ provider.Provider = &MonadProvider{}
var _ provider.ProviderWithFunctions = &MonadProvider{}
var _ provider.ProviderWithEphemeralResources = &MonadProvider{}

type MonadProvider struct {
	version        string
	organizationID string
}

type MonadProviderModel struct {
	BaseURL        types.String `tfsdk:"base_url"`
	APIToken       types.String `tfsdk:"api_token"`
	OrganizationID types.String `tfsdk:"organization_id"`
}

func (p *MonadProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "monad"
	resp.Version = p.version
}

func (p *MonadProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Base URL for the Monad API. Can also be set with the MONAD_BASE_URL environment variable.",
				Optional:            true,
			},
			"api_token": schema.StringAttribute{
				MarkdownDescription: "API token for authentication. Can also be set with the MONAD_API_TOKEN environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "Organization ID for all resources. Can also be set with the MONAD_ORGANIZATION_ID environment variable.",
				Optional:            true,
			},
		},
	}
}

func (p *MonadProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data MonadProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	baseURL := os.Getenv("MONAD_BASE_URL")
	if !data.BaseURL.IsNull() {
		baseURL = data.BaseURL.ValueString()
	}

	if baseURL == "" {
		baseURL = "https://beta.monad.com"
	}

	apiToken := os.Getenv("MONAD_API_TOKEN")
	if !data.APIToken.IsNull() {
		apiToken = data.APIToken.ValueString()
	}

	if apiToken == "" {
		resp.Diagnostics.AddError(
			"Unable to find API token",
			"API token cannot be an empty string. Set the api_token attribute in the provider configuration or the MONAD_API_TOKEN environment variable.",
		)
	}

	organizationID := os.Getenv("MONAD_ORGANIZATION_ID")
	if !data.OrganizationID.IsNull() {
		organizationID = data.OrganizationID.ValueString()
	}

	if organizationID == "" {
		resp.Diagnostics.AddError(
			"Unable to find organization ID",
			"Organization ID cannot be an empty string. Set the organization_id attribute in the provider configuration or the MONAD_ORGANIZATION_ID environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	client := client.NewMonadAPIClient(baseURL, apiToken, organizationID, true)
	p.organizationID = organizationID

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *MonadProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewResourceInput,
		NewResourceOutput,
		NewResourceTransform,
		NewResourceSecret,
		NewResourcePipeline,
	}
}

func (p *MonadProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *MonadProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{}
}

func (p *MonadProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MonadProvider{
			version: version,
		}
	}
}
