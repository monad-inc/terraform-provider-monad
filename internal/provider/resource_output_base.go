package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

type BaseOutputResource[T ConnectorResourceModel] struct {
	client     *client.Client
	outputType string
}

func NewBaseOutputResource[T ConnectorResourceModel](outputType string) *BaseOutputResource[T] {
	return &BaseOutputResource[T]{
		outputType: outputType,
	}
}

func (r *BaseOutputResource[T]) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_output_" + r.outputType
}

func (r *BaseOutputResource[T]) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BaseOutputResource[T]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data T
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config := data.GetSettingsAndSecrets()

	request := monad.RoutesV2CreateOutputRequest{
		Name:        data.GetBaseModel().Name.ValueStringPointer(),
		Description: data.GetBaseModel().Description.ValueStringPointer(),
		OutputType:  ptr(r.outputType),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &config.Settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &config.Secrets,
			},
		},
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsPost(ctx, r.client.OrganizationID).
		RoutesV2CreateOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create %s output, got error: %s", r.outputType, err))
		return
	}

	data.GetBaseModel().ID = types.StringValue(*output.Id)

	tflog.Trace(ctx, fmt.Sprintf("created a %s output resource", r.outputType))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseOutputResource[T]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data T
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdGet(ctx, r.client.OrganizationID, data.GetBaseModel().ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read %s output, got error: %s", r.outputType, err))
		return
	}

	data.GetBaseModel().ID = types.StringValue(*output.Id)
	data.GetBaseModel().Name = types.StringValue(*output.Name)
	data.GetBaseModel().Description = types.StringValue(*output.Description)

	if err := data.UpdateFromAPIResponse(output); err != nil {
		resp.Diagnostics.AddError("Parse Error", fmt.Sprintf("Unable to parse %s output response: %s", r.outputType, err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseOutputResource[T]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data T
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config := data.GetSettingsAndSecrets()

	request := monad.RoutesV2PutOutputRequest{
		Name:        data.GetBaseModel().Name.ValueStringPointer(),
		Description: data.GetBaseModel().Description.ValueStringPointer(),
		OutputType:  ptr(r.outputType),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &config.Settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &config.Secrets,
			},
		},
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsOutputIdPut(ctx, r.client.OrganizationID, data.GetBaseModel().ID.ValueString()).
		RoutesV2PutOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update %s output, got error: %s", r.outputType, err))
		return
	}

	data.GetBaseModel().ID = types.StringValue(*output.Id)
	data.GetBaseModel().Name = types.StringValue(*output.Name)
	data.GetBaseModel().Description = types.StringValue(*output.Description)

	if err := data.UpdateFromAPIResponse(output); err != nil {
		resp.Diagnostics.AddError("Parse Error", fmt.Sprintf("Unable to parse %s output response: %s", r.outputType, err))
		return
	}

	tflog.Trace(ctx, fmt.Sprintf("updated a %s output resource", r.outputType))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseOutputResource[T]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data T
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, _, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdDelete(ctx, r.client.OrganizationID, data.GetBaseModel().ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete %s output, got error: %s", r.outputType, err))
		return
	}
}

func (r *BaseOutputResource[T]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
