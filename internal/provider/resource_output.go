package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ResourceOutput{}
var _ ConnectorResourceModel = &ResourceOutputModel{}

func init() {
	RegisteredConnectorResources = append(RegisteredConnectorResources, NewResourceOutput)
}

func NewResourceOutput() resource.Resource {
	return &ResourceOutput{
		BaseOutputResource: NewBaseOutputResource[*ResourceOutputModel](""),
	}
}

type ResourceOutput struct {
	*BaseOutputResource[*ResourceOutputModel]
}

type ResourceOutputModel struct {
	BaseConnectorModel
	ComponentType types.String          `tfsdk:"component_type"`
	Config        *ResourceOutputConfig `tfsdk:"config"`
}

type ResourceOutputConfig struct {
	Settings types.Dynamic `tfsdk:"settings"`
	Secrets  types.Dynamic `tfsdk:"secrets"`
}

func (r *ResourceOutput) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Generic Input",

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
			"component_type": schema.StringAttribute{
				MarkdownDescription: "Type of the input component",
				Required:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "Generic Input configuration",
				Attributes: map[string]schema.Attribute{
					"settings": schema.DynamicAttribute{
						MarkdownDescription: "Settings for the input",
						Optional:            true,
					},
					"secrets": schema.DynamicAttribute{
						MarkdownDescription: "Settings for the input",
						Optional:            true,
						Sensitive:           true,
					},
				},
			},
		},
	}
}

func (m *ResourceOutputModel) GetComponentSubType() string {
	if m.ComponentType.IsNull() || m.ComponentType.IsUnknown() {
		return ""
	}
	return m.ComponentType.ValueString()
}

func (m *ResourceOutputModel) GetBaseModel() *BaseConnectorModel {
	return &m.BaseConnectorModel
}

func (m *ResourceOutputModel) GetSettingsAndSecrets(ctx context.Context) (*BaseConnectorConfig, error) {
	config := &BaseConnectorConfig{
		Settings: make(map[string]any),
		Secrets:  make(map[string]any),
	}

	if m.Config == nil {
		return config, nil
	}

	if !m.Config.Settings.IsNull() {
		settings, err := tfDynamicToMapAny(m.Config.Settings)
		if err != nil {
			return nil, err
		}
		if settings != nil {
			config.Settings = settings
		}
	}
	if !m.Config.Secrets.IsNull() {
		secrets, err := tfDynamicToMapAny(m.Config.Secrets)
		if err != nil {
			return nil, err
		}
		if secrets != nil {
			config.Secrets = secrets
		}
	}

	return config, nil
}

func (m *ResourceOutputModel) UpdateFromAPIResponse(output any) error {
	// Since we can't determine the exact type, we'll use type assertions
	// The actual type will need to be determined from the monad SDK
	// For now, this is a placeholder that needs to be implemented properly
	return nil
}
