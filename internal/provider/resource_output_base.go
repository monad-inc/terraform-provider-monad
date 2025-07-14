package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &BaseOutputResource[ConnectorResourceModel]{}

type BaseOutputResource[T ConnectorResourceModel] struct {
	client     *client.Client
	outputType string
}

func NewBaseOutputResource[T ConnectorResourceModel](outputType string) *BaseOutputResource[T] {
	return &BaseOutputResource[T]{
		outputType: outputType,
	}
}

func (r *BaseOutputResource[T]) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = getConnectorTypeName(req.ProviderTypeName, "output", r.outputType)
}

func (r *BaseOutputResource[T]) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Diagnostics.AddError("Not implemented", "Schema is not implemented")
}

func (r *BaseOutputResource[T]) Configure(
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

func (r *BaseOutputResource[T]) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data T
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, err := data.GetSettingsAndSecrets(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get settings and secrets", err.Error())
		return
	}

	request := monad.RoutesV2CreateOutputRequest{
		Name:        data.GetBaseModel().Name.ValueStringPointer(),
		Description: data.GetBaseModel().Description.ValueStringPointer(),
		OutputType:  ptr(data.GetComponentSubType()),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &config.Settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &config.Secrets,
			},
		},
	}

	output, monadResp, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsPost(ctx, r.client.OrganizationID).
		RoutesV2CreateOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create %s output, got error: %s. Response: %s",
				r.outputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.GetBaseModel().ID = types.StringValue(*output.Id)

	tflog.Trace(ctx, fmt.Sprintf("created a %s output resource", r.outputType))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseOutputResource[T]) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data T
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, monadResp, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdGet(
			ctx,
			r.client.OrganizationID,
			data.GetBaseModel().ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read %s output, got error: %s. Response: %s",
				r.outputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.GetBaseModel().ID = types.StringValue(*output.Id)
	data.GetBaseModel().Name = types.StringValue(*output.Name)
	data.GetBaseModel().Description = types.StringValue(*output.Description)

	if err := data.UpdateFromAPIResponse(output); err != nil {
		resp.Diagnostics.AddError(
			"Parse Error",
			fmt.Sprintf("Unable to parse %s output response: %s", r.outputType, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseOutputResource[T]) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data T
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, err := data.GetSettingsAndSecrets(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get settings and secrets", err.Error())
		return
	}

	request := monad.RoutesV2PutOutputRequest{
		Name:        data.GetBaseModel().Name.ValueStringPointer(),
		Description: data.GetBaseModel().Description.ValueStringPointer(),
		OutputType:  ptr(data.GetComponentSubType()),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &config.Settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &config.Secrets,
			},
		},
	}

	output, monadResp, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsOutputIdPut(
			ctx,
			r.client.OrganizationID,
			data.GetBaseModel().ID.ValueString(),
		).
		RoutesV2PutOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update %s output, got error: %s. Response: %s",
				r.outputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.GetBaseModel().ID = types.StringValue(*output.Id)
	data.GetBaseModel().Name = types.StringValue(*output.Name)
	data.GetBaseModel().Description = types.StringValue(*output.Description)

	if err := data.UpdateFromAPIResponse(output); err != nil {
		resp.Diagnostics.AddError(
			"Parse Error",
			fmt.Sprintf("Unable to parse %s output response: %s", r.outputType, err),
		)
		return
	}

	tflog.Trace(ctx, fmt.Sprintf("updated a %s output resource", r.outputType))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseOutputResource[T]) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data T
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdDelete(
			ctx,
			r.client.OrganizationID,
			data.GetBaseModel().ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete %s output, got error: %s. Response: %s",
				r.outputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}
