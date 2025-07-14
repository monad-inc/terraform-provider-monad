package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// @TODO - Check if there is a way to use the monad SDK types directly instead of defining our own types here.
//   - Does the terraform-plugin-framework support marshalling and unmarshalling of SDK types directly?
//   - Does the terraform-plugin-framework support unstructured types
//   - Look into using SDKs ToMap() method to convert between SDK types and framework types.
//   - Add CRUD methods to client wrapper

var _ resource.Resource = &ResourceOutputHTTP{}
var _ ConnectorResourceModel = &ResourceOutputHTTPModel{}

func init() {
	RegisteredConnectorResources = append(RegisteredConnectorResources, NewResourceOutputHTTP)
}

func NewResourceOutputHTTP() resource.Resource {
	return &ResourceOutputHTTP{
		BaseOutputResource: NewBaseOutputResource[*ResourceOutputHTTPModel]("http"),
	}
}

type ResourceOutputHTTP struct {
	*BaseOutputResource[*ResourceOutputHTTPModel]
}

type ResourceOutputHTTPModel struct {
	BaseConnectorModel
	Config *ResourceOutputHTTPConfig `tfsdk:"config"`
}

type ResourceOutputHTTPConfig struct {
	Settings *ResourceOutputHTTPSettings `tfsdk:"settings"`
	Secrets  *ResourceOutputHTTPSecrets  `tfsdk:"secrets"`
}

type ResourceOutputHTTPSettings struct {
	Endpoint            types.String                `tfsdk:"endpoint"`
	Method              types.String                `tfsdk:"method"`
	Headers             []ResourceOutputHTTPHeaders `tfsdk:"headers"`
	MaxBatchDataSize    types.Float64               `tfsdk:"max_batch_data_size"`
	MaxBatchRecordCount types.Int64                 `tfsdk:"max_batch_record_count"`
	PayloadStructure    types.String                `tfsdk:"payload_structure"`
	RateLimit           types.Int64                 `tfsdk:"rate_limit"`
	TLSSkipVerify       types.Bool                  `tfsdk:"tls_skip_verify"`
	WrapperKey          types.String                `tfsdk:"wrapper_key"`
}

type ResourceOutputHTTPHeaders struct {
	Key   string `json:"header_key" tfsdk:"key"`
	Value string `json:"header_value" tfsdk:"value"`
}

type ResourceOutputHTTPSecrets struct {
	AuthHeaders map[string]types.String `tfsdk:"auth_headers"`
}

func (r *ResourceOutputHTTP) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "HTTP output resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Output identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the output",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the output",
				Optional:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "HTTP output configuration",
				Blocks: map[string]schema.Block{
					"settings": schema.SingleNestedBlock{
						MarkdownDescription: "HTTP settings configuration",
						Attributes: map[string]schema.Attribute{
							"endpoint": schema.StringAttribute{
								MarkdownDescription: "The full URL of the HTTP endpoint to send data to",
								Required:            true,
							},
							"method": schema.StringAttribute{
								MarkdownDescription: "The HTTP method to use for requests (GET, POST, PUT, PATCH, or DELETE)",
								Optional:            true,
							},
							"headers": schema.ListAttribute{
								MarkdownDescription: "Non-secret headers",
								ElementType: types.ObjectType{
									AttrTypes: map[string]attr.Type{
										"key":   types.StringType,
										"value": types.StringType,
									},
								},
								Optional: true,
							},
							"max_batch_data_size": schema.Float64Attribute{
								MarkdownDescription: "The maximum size in KB for a single batch of data",
								Optional:            true,
							},
							"max_batch_record_count": schema.Int64Attribute{
								MarkdownDescription: "The maximum number of records to include in a single batch",
								Optional:            true,
							},
							"payload_structure": schema.StringAttribute{
								MarkdownDescription: "The payload structure type",
								Optional:            true,
							},
							"rate_limit": schema.Int64Attribute{
								MarkdownDescription: "Maximum number of requests per second to send to the endpoint",
								Optional:            true,
							},
							"tls_skip_verify": schema.BoolAttribute{
								MarkdownDescription: "Skip TLS verification",
								Optional:            true,
							},
							"wrapper_key": schema.StringAttribute{
								MarkdownDescription: "The key to use for wrapping the payload when PayloadStructure is set to 'wrapped'",
								Optional:            true,
							},
						},
					},
					"secrets": schema.SingleNestedBlock{
						MarkdownDescription: "HTTP secrets configuration",
						Attributes: map[string]schema.Attribute{
							"auth_headers": schema.MapAttribute{
								MarkdownDescription: "Authentication headers",
								ElementType:         types.StringType,
								Optional:            true,
								Sensitive:           true,
							},
						},
					},
				},
			},
		},
	}
}

func (m *ResourceOutputHTTPModel) GetBaseModel() *BaseConnectorModel {
	return &m.BaseConnectorModel
}

func (m *ResourceOutputHTTPModel) GetSettingsAndSecrets() BaseConnectorConfig {
	config := BaseConnectorConfig{
		Settings: make(map[string]any),
		Secrets:  make(map[string]any),
	}

	if m.Config.Settings != nil {
		if !m.Config.Settings.Endpoint.IsNull() {
			config.Settings["endpoint"] = m.Config.Settings.Endpoint.ValueString()
		}
		if !m.Config.Settings.Method.IsNull() {
			config.Settings["method"] = m.Config.Settings.Method.ValueString()
		}
		if m.Config.Settings.Headers != nil {
			config.Settings["headers"] = m.Config.Settings.Headers
		}
		if !m.Config.Settings.MaxBatchDataSize.IsNull() {
			config.Settings["max_batch_data_size"] = m.Config.Settings.MaxBatchDataSize.ValueFloat64()
		}
		if !m.Config.Settings.MaxBatchRecordCount.IsNull() {
			config.Settings["max_batch_record_count"] = m.Config.Settings.MaxBatchRecordCount.ValueInt64()
		}
		if !m.Config.Settings.PayloadStructure.IsNull() {
			config.Settings["payload_structure"] = m.Config.Settings.PayloadStructure.ValueString()
		}
		if !m.Config.Settings.RateLimit.IsNull() {
			config.Settings["rate_limit"] = m.Config.Settings.RateLimit.ValueInt64()
		}
		if !m.Config.Settings.TLSSkipVerify.IsNull() {
			config.Settings["tls_skip_verify"] = m.Config.Settings.TLSSkipVerify.ValueBool()
		}
		if !m.Config.Settings.WrapperKey.IsNull() {
			config.Settings["wrapper_key"] = m.Config.Settings.WrapperKey.ValueString()
		}
	}

	if m.Config.Secrets != nil {
		if m.Config.Secrets.AuthHeaders != nil {
			config.Secrets["auth_headers"] = m.Config.Secrets.AuthHeaders
		}
	}

	return config
}

func (m *ResourceOutputHTTPModel) UpdateFromAPIResponse(output any) error {
	// Since we can't determine the exact type, we'll use type assertions
	// The actual type will need to be determined from the monad SDK
	// For now, this is a placeholder that needs to be implemented properly
	return nil
}
