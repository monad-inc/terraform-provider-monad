package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// @TODO - Check if there is a way to use the monad SDK types directly instead of defining our own types here.
//   - Does the terraform-plugin-framework support marshalling and unmarshalling of SDK types directly?
//   - Does the terraform-plugin-framework support unstructured types
//   - Look into using SDKs ToMap() method to convert between SDK types and framework types.
//   - Add CRUD methods to client wrapper

var _ resource.Resource = &ResourceOutputHTTP{}
var _ resource.ResourceWithImportState = &ResourceOutputHTTP{}

func NewResourceOutputHTTP() resource.Resource {
	return &ResourceOutputHTTP{}
}

type ResourceOutputHTTP struct {
	client *client.Client
}

type ResourceOutputHTTPModel struct {
	ID          types.String              `tfsdk:"id"`
	Name        types.String              `tfsdk:"name"`
	Description types.String              `tfsdk:"description"`
	Config      *ResourceOutputHTTPConfig `tfsdk:"config"`
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

func (r *ResourceOutputHTTP) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_output_http"
}

func (r *ResourceOutputHTTP) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
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

func (r *ResourceOutputHTTP) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clientData, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *ClientData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = clientData
}

func (r *ResourceOutputHTTP) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ResourceOutputHTTPModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Config == nil {
		resp.Diagnostics.AddError("Missing Config", "The config block is required for the HTTP output resource.")
		return
	}

	settings, secrets := r.getSettingsAndSecretsFromConfig(&data)

	request := monad.RoutesV2CreateOutputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		OutputType:  ptr("http"),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsPost(ctx, r.client.OrganizationID).
		RoutesV2CreateOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP output, got error: %s", err))
		return
	}

	data.ID = types.StringValue(*output.Id)

	tflog.Trace(ctx, "created an HTTP output resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutputHTTP) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ResourceOutputHTTPModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdGet(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read HTTP output, got error: %s", err))
		return
	}

	b, _ := json.MarshalIndent(output, "", "  ")
	fmt.Printf("[Debug] Read HTTP output: %+v\n", string(b))
	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = types.StringValue(*output.Description)
	data.Config = &ResourceOutputHTTPConfig{
		Settings: &ResourceOutputHTTPSettings{
			Endpoint: types.StringValue(output.Config.Settings["endpoint"].(string)),
			// Method:              types.StringValue(output.Config.Settings["method"].(string)),
			// MaxBatchDataSize:    types.Float64Value(output.Config.Settings["max_batch_data_size"].(float64)),
			// MaxBatchRecordCount: types.Int64Value(output.Config.Settings["max_batch_record_count"].(int64)),
			// PayloadStructure:    types.StringValue(output.Config.Settings["payload_structure"].(string)),
			// RateLimit:           types.Int64Value(output.Config.Settings["rate_limit"].(int64)),
			// TLSSkipVerify:       types.BoolValue(output.Config.Settings["tls_skip_verify"].(bool)),
			// WrapperKey:          types.StringValue(output.Config.Settings["wrapper_key"].(string)),
		},
		Secrets: &ResourceOutputHTTPSecrets{
			// AuthHeaders: authHeaders,
		},
	}

	if headers, ok := output.Config.Settings["headers"].([]any); ok {
		data.Config.Settings.Headers = make([]ResourceOutputHTTPHeaders, 0, len(headers))
		for _, header := range headers {
			if headerMap, ok := header.(map[string]any); ok {
				data.Config.Settings.Headers = append(data.Config.Settings.Headers, ResourceOutputHTTPHeaders{
					Key:   headerMap["header_key"].(string),
					Value: headerMap["header_value"].(string),
				})
			}
		}
	}

	b, _ = json.MarshalIndent(data, "", "  ")
	fmt.Printf("[Debug] %s\n", string(b))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutputHTTP) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ResourceOutputHTTPModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Config == nil {
		resp.Diagnostics.AddError("Missing Config", "The config block is required for the HTTP output resource.")
		return
	}

	settings, secrets := r.getSettingsAndSecretsFromConfig(&data)

	request := monad.RoutesV2PutOutputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		OutputType:  ptr("http"),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsOutputIdPut(ctx, r.client.OrganizationID, data.ID.ValueString()).
		RoutesV2PutOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update HTTP output, got error: %s", err))
		return
	}

	headers := make([]ResourceOutputHTTPHeaders, 0, len(output.Config.Settings["headers"].(map[string]string)))
	for k, v := range output.Config.Settings["headers"].(map[string]string) {
		headers = append(headers, ResourceOutputHTTPHeaders{
			Key:   k,
			Value: v,
		})
	}

	authHeaders := make(map[string]types.String, len(output.Config.Secrets["auth_headers"].(map[string]string)))
	for k, v := range output.Config.Secrets["auth_headers"].(map[string]string) {
		authHeaders[k] = types.StringValue(v)
	}

	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = types.StringValue(*output.Description)
	data.Config = &ResourceOutputHTTPConfig{
		Settings: &ResourceOutputHTTPSettings{
			Endpoint:            types.StringValue(output.Config.Settings["endpoint"].(string)),
			Method:              types.StringValue(output.Config.Settings["method"].(string)),
			Headers:             headers,
			MaxBatchDataSize:    types.Float64Value(output.Config.Settings["max_batch_data_size"].(float64)),
			MaxBatchRecordCount: types.Int64Value(output.Config.Settings["max_batch_record_count"].(int64)),
			PayloadStructure:    types.StringValue(output.Config.Settings["payload_structure"].(string)),
			RateLimit:           types.Int64Value(output.Config.Settings["rate_limit"].(int64)),
			TLSSkipVerify:       types.BoolValue(output.Config.Settings["tls_skip_verify"].(bool)),
			WrapperKey:          types.StringValue(output.Config.Settings["wrapper_key"].(string)),
		},
		Secrets: &ResourceOutputHTTPSecrets{
			AuthHeaders: authHeaders,
		},
	}

	tflog.Trace(ctx, "updated an HTTP output resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutputHTTP) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ResourceOutputHTTPModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, _, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdDelete(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete HTTP output, got error: %s", err))
		return
	}
}

func (r *ResourceOutputHTTP) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ResourceOutputHTTP) getSettingsAndSecretsFromConfig(config *ResourceOutputHTTPModel) (map[string]any, map[string]any) {
	settings := make(map[string]any)
	secrets := make(map[string]any)

	if config.Config.Settings != nil {
		if !config.Config.Settings.Endpoint.IsNull() {
			settings["endpoint"] = config.Config.Settings.Endpoint.ValueString()
		}
		if !config.Config.Settings.Method.IsNull() {
			settings["method"] = config.Config.Settings.Method.ValueString()
		}
		if config.Config.Settings.Headers != nil {
			settings["headers"] = config.Config.Settings.Headers
		}
		if !config.Config.Settings.MaxBatchDataSize.IsNull() {
			settings["max_batch_data_size"] = config.Config.Settings.MaxBatchDataSize.ValueFloat64()
		}
		if !config.Config.Settings.MaxBatchRecordCount.IsNull() {
			settings["max_batch_record_count"] = config.Config.Settings.MaxBatchRecordCount.ValueInt64()
		}
		if !config.Config.Settings.PayloadStructure.IsNull() {
			settings["payload_structure"] = config.Config.Settings.PayloadStructure.ValueString()
		}
		if !config.Config.Settings.RateLimit.IsNull() {
			settings["rate_limit"] = config.Config.Settings.RateLimit.ValueInt64()
		}
		if !config.Config.Settings.TLSSkipVerify.IsNull() {
			settings["tls_skip_verify"] = config.Config.Settings.TLSSkipVerify.ValueBool()
		}
		if !config.Config.Settings.WrapperKey.IsNull() {
			settings["wrapper_key"] = config.Config.Settings.WrapperKey.ValueString()
		}
	}

	if config.Config.Secrets != nil {
		if config.Config.Secrets.AuthHeaders != nil {
			secrets["auth_headers"] = config.Config.Secrets.AuthHeaders
		}
	}

	return settings, secrets
}
