package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ResourceInputDemo{}
var _ ConnectorResourceModel = &ResourceInputDemoModel{}

func init() {
	RegisteredConnectorResources = append(RegisteredConnectorResources, NewResourceInputDemo)
}

func NewResourceInputDemo() resource.Resource {
	return &ResourceInputDemo{
		BaseInputResource: NewBaseInputResource[*ResourceInputDemoModel]("demo"),
	}
}

type ResourceInputDemo struct {
	*BaseInputResource[*ResourceInputDemoModel]
}

type ResourceInputDemoModel struct {
	BaseConnectorModel
	Config *ResourceInputDemoConfig `tfsdk:"config"`
}

type ResourceInputDemoConfig struct {
	Settings *ResourceInputDemoConfigSettings `tfsdk:"settings"`
}

type ResourceInputDemoConfigSettings struct {
	RecordType types.String `tfsdk:"record_type"`
	Rate       types.Int32  `tfsdk:"rate"`
}

func (r *ResourceInputDemo) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Event Generator",

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
				MarkdownDescription: "Event Generator configuration",
				Blocks: map[string]schema.Block{
					"settings": schema.SingleNestedBlock{
						MarkdownDescription: "Event Generator settings configuration",
						Attributes: map[string]schema.Attribute{
							"record_type": schema.StringAttribute{
								MarkdownDescription: "The type of record to generate",
								Required:            true,
							},
							"rate": schema.Int32Attribute{
								MarkdownDescription: "The rate at which to generate records (between 1 and 1000) per second",
								Required:            true,
							},
						},
					},
				},
			},
		},
	}
}

func (m *ResourceInputDemoModel) GetComponentSubType() string {
	return "demo"
}

func (m *ResourceInputDemoModel) GetBaseModel() *BaseConnectorModel {
	return &m.BaseConnectorModel
}

func (m *ResourceInputDemoModel) GetSettingsAndSecrets(ctx context.Context) (*BaseConnectorConfig, error) {
	config := &BaseConnectorConfig{
		Settings: make(map[string]any),
		Secrets:  make(map[string]any),
	}

	if m.Config.Settings != nil {
		if !m.Config.Settings.RecordType.IsNull() {
			config.Settings["record_type"] = m.Config.Settings.RecordType.ValueString()
		}
		if !m.Config.Settings.Rate.IsNull() {
			config.Settings["rate"] = m.Config.Settings.Rate.ValueInt32()
		}
	}

	return config, nil
}

func (m *ResourceInputDemoModel) UpdateFromAPIResponse(output any) error {
	// Since we can't determine the exact type, we'll use type assertions
	// The actual type will need to be determined from the monad SDK
	// For now, this is a placeholder that needs to be implemented properly
	return nil
}
