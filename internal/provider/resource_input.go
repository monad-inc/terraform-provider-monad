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

var _ resource.Resource = &ResourceInput{}
var _ resource.ResourceWithConfigure = &ResourceInput{}
var _ resource.ResourceWithImportState = &ResourceInput{}
var _ resource.ResourceWithModifyPlan = &ResourceInput{}

func NewResourceInput() resource.Resource {
	return &ResourceInput{}
}

type ResourceInput struct {
	client *client.Client
}

func (r *ResourceInput) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = fmt.Sprintf("%s_input", req.ProviderTypeName)
}

func (r *ResourceInput) Configure(
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

func (r *ResourceInput) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = getConnectorSchema()
}

func (r *ResourceInput) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, secrets, err := data.getSettingsAndSecrets()
	if err != nil {
		resp.Diagnostics.AddError("Failed to get settings and secrets", err.Error())
		return
	}

	request := monad.RoutesV2CreateInputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Type:        data.ComponentType.ValueStringPointer(),
		Config: &monad.SecretProcessesorInputConfig{
			Settings: &monad.SecretProcessesorInputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorInputConfigSecrets{
				MapmapOfStringAny: &secrets,
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
				"Unable to create input, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Only the computed `id` is taken from the response; name/description/type/
	// settings are plan-known and must be returned unchanged (their planned cty
	// type must be preserved — rebuilding from the response trips "Provider
	// produced inconsistent result after apply"). Secrets are write-only, so
	// they are nulled in state and fingerprinted into secrets_hash.
	data.ID = types.StringValue(*input.Id)
	if err := finalizeConnectorSecrets(ctx, r.client.OrganizationID, &data, secrets); err != nil {
		resp.Diagnostics.AddError("Failed to fingerprint input secrets", err.Error())
		return
	}

	tflog.Trace(ctx, "created an input resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceInput) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input, monadResp, err := r.client.OrganizationInputsAPI.
		V1OrganizationIdInputsInputIdGet(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read input, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	description := types.StringNull()
	if input.Description != nil && *input.Description != "" {
		description = types.StringValue(*input.Description)
	}

	data.ID = types.StringValue(*input.Id)
	data.Name = types.StringValue(*input.Name)
	data.Description = description
	data.ComponentType = types.StringValue(*input.Type)
	if err := refreshConnectorSettings(&data, input.Config.Settings); err != nil {
		resp.Diagnostics.AddError("Failed to refresh input settings", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceInput) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Write-only `secrets` are null in the plan; read them from the
	// configuration, which is the only place their values are available.
	var cfg ResourceConnectorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, secrets, err := cfg.getSettingsAndSecrets()
	if err != nil {
		resp.Diagnostics.AddError("Failed to get settings and secrets", err.Error())
		return
	}

	request := monad.RoutesV2PutInputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Type:        data.ComponentType.ValueStringPointer(),
		Config: &monad.SecretProcessesorInputConfig{
			Settings: &monad.SecretProcessesorInputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorInputConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	_, monadResp, err := r.client.OrganizationInputsAPI.
		V2OrganizationIdInputsInputIdPut(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		RoutesV2PutInputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update input, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Preserve plan-known values (see Create); data already holds
	// id/name/description/type/settings from the plan. Secrets stay write-only
	// (nulled) and secrets_hash is refreshed to match what was just sent.
	if err := finalizeConnectorSecrets(ctx, r.client.OrganizationID, &data, secrets); err != nil {
		resp.Diagnostics.AddError("Failed to fingerprint input secrets", err.Error())
		return
	}

	tflog.Trace(ctx, "updated an input resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceInput) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if r.client == nil {
		return
	}
	modifyConnectorPlanForSecrets(ctx, r.client.OrganizationID, req, resp)
}

func (r *ResourceInput) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationInputsAPI.
		V1OrganizationIdInputsInputIdDelete(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete input, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}

func (r *ResourceInput) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
