package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ResourceInputOktaSystemAuditLogs{}
var _ ConnectorResourceModel = &ResourceInputOktaSystemAuditLogsModel{}

func init() {
	RegisteredConnectorResources = append(RegisteredConnectorResources, NewResourceInputOktaSystemAuditLogs)
}

func NewResourceInputOktaSystemAuditLogs() resource.Resource {
	return &ResourceInputOktaSystemAuditLogs{
		BaseInputResource: NewBaseInputResource[*ResourceInputOktaSystemAuditLogsModel]("okta-systemlog"),
	}
}

type ResourceInputOktaSystemAuditLogs struct {
	*BaseInputResource[*ResourceInputOktaSystemAuditLogsModel]
}

type ResourceInputOktaSystemAuditLogsModel struct {
	BaseConnectorModel
	Config *ResourceInputOktaSystemAuditLogsConfig `tfsdk:"config"`
}

type ResourceInputOktaSystemAuditLogsConfig struct {
	Settings *ResourceInputOktaSystemAuditLogsConfigSettings `tfsdk:"settings"`
	Secrets  *ResourceInputOktaSystemAuditLogsConfigSecrets  `tfsdk:"secrets"`
}

type ResourceInputOktaSystemAuditLogsConfigSettings struct {
	OrgURL types.String `tfsdk:"org_url"`
}

type ResourceInputOktaSystemAuditLogsConfigSecrets struct {
	APIKey ConnectorSecret `tfsdk:"api_key"`
}

func (r *ResourceInputOktaSystemAuditLogs) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Okta System Audit Logs Input Resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Input identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the input",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the input",
				Optional:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "Okta System Audit Logs configuration",
				Blocks: map[string]schema.Block{
					"settings": schema.SingleNestedBlock{
						MarkdownDescription: "Okta System Audit Logs settings configuration",
						Attributes: map[string]schema.Attribute{
							"org_url": schema.StringAttribute{
								MarkdownDescription: "The Okta organization URL",
								Required:            true,
							},
						},
					},
					"secrets": schema.SingleNestedBlock{
						MarkdownDescription: "Okta System Audit Logs secrets configuration",
						Attributes: map[string]schema.Attribute{
							"api_key": schema.SingleNestedAttribute{
								MarkdownDescription: "API Key for Okta System Audit Logs",
								Required:            true,
								Attributes: map[string]schema.Attribute{
									"id": schema.StringAttribute{
										MarkdownDescription: "Secret identifier",
										Optional:            true,
									},
									"name": schema.StringAttribute{
										MarkdownDescription: "Secret name",
										Optional:            true,
									},
									"description": schema.StringAttribute{
										MarkdownDescription: "Secret description",
										Optional:            true,
									},
									"value": schema.StringAttribute{
										MarkdownDescription: "Secret value",
										Optional:            true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (m *ResourceInputOktaSystemAuditLogsModel) GetBaseModel() *BaseConnectorModel {
	return &m.BaseConnectorModel
}

func (m *ResourceInputOktaSystemAuditLogsModel) GetSettingsAndSecrets() BaseConnectorConfig {
	config := BaseConnectorConfig{
		Settings: make(map[string]any),
		Secrets:  make(map[string]any),
	}

	if m.Config.Settings != nil {
		if !m.Config.Settings.OrgURL.IsNull() {
			config.Settings["org_url"] = m.Config.Settings.OrgURL.ValueString()
		}
	}
	if m.Config.Secrets != nil {
		apiKeySecret := make(map[string]any)
		if !m.Config.Secrets.APIKey.ID.IsNull() {
			apiKeySecret["id"] = m.Config.Secrets.APIKey.ID.ValueString()
		}
		if !m.Config.Secrets.APIKey.Name.IsNull() {
			apiKeySecret["name"] = m.Config.Secrets.APIKey.Name.ValueString()
		}
		if !m.Config.Secrets.APIKey.Description.IsNull() {
			apiKeySecret["description"] = m.Config.Secrets.APIKey.Description.ValueString()
		}
		if !m.Config.Secrets.APIKey.Value.IsNull() {
			apiKeySecret["value"] = m.Config.Secrets.APIKey.Value.ValueString()
		}
		config.Secrets["api_key"] = apiKeySecret
	}

	return config
}

func (m *ResourceInputOktaSystemAuditLogsModel) UpdateFromAPIResponse(output any) error {
	// Since we can't determine the exact type, we'll use type assertions
	// The actual type will need to be determined from the monad SDK
	// For now, this is a placeholder that needs to be implemented properly
	return nil
}
