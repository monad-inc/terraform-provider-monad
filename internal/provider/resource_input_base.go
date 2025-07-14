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

var _ resource.Resource = &BaseInputResource[ConnectorResourceModel]{}

type BaseInputResource[T ConnectorResourceModel] struct {
	client    *client.Client
	inputType string
}

func NewBaseInputResource[T ConnectorResourceModel](inputType string) *BaseInputResource[T] {
	return &BaseInputResource[T]{
		inputType: inputType,
	}
}

func (r *BaseInputResource[T]) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = getConnectorTypeName(req.ProviderTypeName, "input", r.inputType)
}

func (r *BaseInputResource[T]) Configure(
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

func (r *BaseInputResource[T]) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Diagnostics.AddError("Not implemented", "Schema is not implemented")
}

func (r *BaseInputResource[T]) Create(
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

	request := monad.RoutesV2CreateInputRequest{
		Name:        data.GetBaseModel().Name.ValueStringPointer(),
		Description: data.GetBaseModel().Description.ValueStringPointer(),
		Type:        ptr(data.GetComponentSubType()),
		Config: &monad.SecretProcessesorInputConfig{
			Settings: &monad.SecretProcessesorInputConfigSettings{
				MapmapOfStringAny: &config.Settings,
			},
			Secrets: &monad.SecretProcessesorInputConfigSecrets{
				MapmapOfStringAny: &config.Secrets,
			},
		},
	}

	input, monadResp, err := r.client.OrganizationInputsAPI.
		V2OrganizationIdInputsPost(ctx, r.client.OrganizationID).
		RoutesV2CreateInputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create %s input, got error: %s. Response: %s",
				r.inputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.GetBaseModel().ID = types.StringValue(*input.Id)

	tflog.Trace(ctx, fmt.Sprintf("created a %s input resource", r.inputType))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseInputResource[T]) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data T
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, monadResp, err := r.client.OrganizationInputsAPI.
		V1OrganizationIdInputsInputIdGet(
			ctx,
			r.client.OrganizationID,
			data.GetBaseModel().ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read %s input, got error: %s. Response: %s",
				r.inputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.GetBaseModel().ID = types.StringValue(*input.Id)
	data.GetBaseModel().Name = types.StringValue(*input.Name)
	data.GetBaseModel().Description = types.StringValue(*input.Description)

	if err := data.UpdateFromAPIResponse(input); err != nil {
		resp.Diagnostics.AddError(
			"Parse Error",
			fmt.Sprintf("Unable to parse %s input response: %s", r.inputType, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseInputResource[T]) Update(
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

	request := monad.RoutesV2PutInputRequest{
		Name:        data.GetBaseModel().Name.ValueStringPointer(),
		Description: data.GetBaseModel().Description.ValueStringPointer(),
		Type:        ptr(data.GetComponentSubType()),
		Config: &monad.SecretProcessesorInputConfig{
			Settings: &monad.SecretProcessesorInputConfigSettings{
				MapmapOfStringAny: &config.Settings,
			},
			Secrets: &monad.SecretProcessesorInputConfigSecrets{
				MapmapOfStringAny: &config.Secrets,
			},
		},
	}

	input, monadResp, err := r.client.OrganizationInputsAPI.
		V2OrganizationIdInputsInputIdPut(
			ctx,
			r.client.OrganizationID,
			data.GetBaseModel().ID.ValueString(),
		).
		RoutesV2PutInputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update %s input, got error: %s. Response: %s",
				r.inputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.GetBaseModel().ID = types.StringValue(*input.Id)
	data.GetBaseModel().Name = types.StringValue(*input.Name)
	data.GetBaseModel().Description = types.StringValue(*input.Description)

	if err := data.UpdateFromAPIResponse(input); err != nil {
		resp.Diagnostics.AddError(
			"Parse Error",
			fmt.Sprintf("Unable to parse %s input response: %s", r.inputType, err),
		)
		return
	}

	tflog.Trace(ctx, fmt.Sprintf("updated a %s input resource", r.inputType))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaseInputResource[T]) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data T
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationInputsAPI.
		V1OrganizationIdInputsInputIdDelete(
			ctx,
			r.client.OrganizationID,
			data.GetBaseModel().ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete %s input, got error: %s. Response: %s",
				r.inputType,
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}
