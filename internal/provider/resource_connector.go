package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceConnectorModel struct {
	ID            types.String             `tfsdk:"id"`
	Name          types.String             `tfsdk:"name"`
	Description   types.String             `tfsdk:"description"`
	ComponentType types.String             `tfsdk:"type"`
	Config        *ResourceConnectorConfig `tfsdk:"config"`
}

type ResourceConnectorConfig struct {
	Settings types.Dynamic `tfsdk:"settings"`
	Secrets  types.Dynamic `tfsdk:"secrets"`
}

type ConnectorSecret struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Value       types.String `tfsdk:"value"`
}

func (m *ResourceConnectorModel) getSettingsAndSecrets() (map[string]any, map[string]any, error) {
	settings := make(map[string]any)
	secrets := make(map[string]any)

	if m.Config == nil {
		return settings, secrets, nil
	}

	var err error
	if !m.Config.Settings.IsNull() {
		settings, err = tfDynamicToMapAny(m.Config.Settings)
		if err != nil {
			return nil, nil, err
		}
	}
	if !m.Config.Secrets.IsNull() {
		secrets, err = tfDynamicToMapAny(m.Config.Secrets)
		if err != nil {
			return nil, nil, err
		}
	}

	return settings, secrets, nil
}

func getConnectorSchema() schema.Schema {
	return schema.Schema{
		MarkdownDescription: "Monad Connector",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Monad ConnectorIdentifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connector",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the connector",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of the connector component",
				Required:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "Connector configuration",
				Attributes: map[string]schema.Attribute{
					"settings": schema.DynamicAttribute{
						MarkdownDescription: "Settings for the connector",
						Optional:            true,
					},
					"secrets": schema.DynamicAttribute{
						MarkdownDescription: "Settings for the connector",
						Optional:            true,
						Sensitive:           true,
					},
				},
			},
		},
	}
}
