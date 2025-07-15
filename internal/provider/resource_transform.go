package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourceTransform{}

type ResourceTransform struct {
	client *client.Client
}

type ResourceTransformModel struct {
	ID          types.String            `tfsdk:"id"`
	Name        types.String            `tfsdk:"name"`
	Description types.String            `tfsdk:"description"`
	Config      ResourceTransformConfig `tfsdk:"config"`
}

type ResourceTransformConfig struct {
	Operations types.Dynamic `tfsdk:"operations"`
}

func NewResourceTransform() resource.Resource {
	return &ResourceTransform{}
}

func (r *ResourceTransform) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_transform"
}

func (r *ResourceTransform) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	clientData, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *ClientData, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = clientData
}

func (r *ResourceTransform) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Secret",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Transform identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the transform",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the transform",
				Optional:            true,
			},
		},
		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "Transform configuration",
				Attributes: map[string]schema.Attribute{
					"operations": schema.DynamicAttribute{
						MarkdownDescription: "List of operations to perform in the transform",
						Required:            true,
					},
				},
			},
		},
	}
}

func (r *ResourceTransform) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourceTransformModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	operations, err := parseOperations(ctx, data.Config.Operations)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to parse operations",
			fmt.Sprintf("Error parsing operations: %s", err.Error()),
		)
		return
	}

	request := monad.RoutesCreateTransformRequest{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueStringPointer(),
		Config: &monad.RoutesTransformConfig{
			Operations: operations,
		},
	}

	transform, monadResp, err := r.client.OrganizationTransformsAPI.
		V1OrganizationIdTransformsPost(
			ctx,
			r.client.OrganizationID,
		).RoutesCreateTransformRequest(request).
		Execute()

	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create transform, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.ID = types.StringValue(*transform.Id)

	tflog.Trace(ctx, "created a transform resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceTransform) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceTransformModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	transform, monadResp, err := r.client.OrganizationTransformsAPI.
		V1OrganizationIdTransformsTransformIdGet(
			ctx,
			data.ID.ValueString(),
			r.client.OrganizationID,
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read transform, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.ID = types.StringValue(*transform.Id)
	data.Name = types.StringValue(*transform.Name)
	data.Description = types.StringValue(*transform.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceTransform) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourceTransformModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	operations, err := parseOperations(ctx, data.Config.Operations)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to parse operations",
			fmt.Sprintf("Error parsing operations: %s", err.Error()),
		)
		return
	}

	request := monad.RoutesUpdateTransformRequest{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueStringPointer(),
		Config: &monad.RoutesTransformConfig{
			Operations: operations,
		},
	}

	transform, monadResp, err := r.client.OrganizationTransformsAPI.
		V1OrganizationIdTransformsTransformIdPatch(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).RoutesUpdateTransformRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update transform, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.ID = types.StringValue(*transform.Id)
	data.Name = types.StringValue(*transform.Name)
	data.Description = types.StringValue(*transform.Description)

	tflog.Trace(ctx, "updated a transform resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceTransform) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceTransformModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationTransformsAPI.
		V1OrganizationIdTransformsTransformIdDelete(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete transform, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}

func parseOperations(_ context.Context, operationsDynamic types.Dynamic) ([]monad.RoutesTransformOperation, error) {
	if operationsDynamic.IsNull() || operationsDynamic.IsUnknown() {
		return nil, nil
	}

	underlying := operationsDynamic.UnderlyingValue()

	var elements []attr.Value
	switch v := underlying.(type) {
	case types.List:
		elements = v.Elements()
	case types.Tuple:
		elements = v.Elements()
	default:
		return nil, fmt.Errorf("operations must be a list or tuple, got %T", underlying)
	}

	operations := make([]monad.RoutesTransformOperation, len(elements))

	for i, element := range elements {
		elementObj, ok := element.(types.Object)
		if !ok {
			return nil, fmt.Errorf("operation at index %d must be an object, got %T", i, element)
		}

		attrs := elementObj.Attributes()

		operationAttr, exists := attrs["operation"]
		if !exists {
			return nil, fmt.Errorf("operation at index %d missing 'operation' field", i)
		}
		operationStr, ok := operationAttr.(types.String)
		if !ok {
			return nil, fmt.Errorf("operation at index %d 'operation' field must be string, got %T", i, operationAttr)
		}

		argumentsAttr, exists := attrs["arguments"]
		if !exists {
			return nil, fmt.Errorf("operation at index %d missing 'arguments' field", i)
		}

		var arguments map[string]any
		var err error
		switch v := argumentsAttr.(type) {
		case types.Dynamic:
			arguments, err = tfDynamicToMapAny(v)
		case types.Object:
			arguments, err = tfObjectToMapAny(context.Background(), v)
		default:
			return nil, fmt.Errorf("operation at index %d 'arguments' field must be dynamic or object, got %T", i, argumentsAttr)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse arguments for operation %d: %w", i, err)
		}

		operations[i] = monad.RoutesTransformOperation{
			Operation: operationStr.ValueStringPointer(),
			Arguments: &monad.RoutesTransformOperationArguments{
				MapmapOfStringAny: &arguments,
			},
		}
	}

	return operations, nil
}
