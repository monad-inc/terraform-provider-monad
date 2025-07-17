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

var _ resource.Resource = &ResourceOutput{}
var _ resource.ResourceWithConfigure = &ResourceOutput{}
var _ resource.ResourceWithImportState = &ResourceOutput{}

func NewResourceOutput() resource.Resource {
	return &ResourceOutput{}
}

type ResourceOutput struct {
	client *client.Client
}

func (r *ResourceOutput) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = fmt.Sprintf("%s_output", req.ProviderTypeName)
}

func (r *ResourceOutput) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = getConnectorSchema()
}

func (r *ResourceOutput) Configure(
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

func (r *ResourceOutput) Create(
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

	request := monad.RoutesV2CreateOutputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		OutputType:  data.ComponentType.ValueStringPointer(),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &secrets,
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
				"Unable to create output, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	config, err := connectorConfigToTF(output.Config.Settings, output.Config.Secrets)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to convert output config",
			fmt.Sprintf("Error converting config: %s", err),
		)
		return
	}

	description := types.StringNull()
	if output.Description != nil && *output.Description != "" {
		description = types.StringValue(*output.Description)
	}

	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = description
	data.ComponentType = types.StringValue(*output.Type)
	data.Config = config

	tflog.Trace(ctx, "created a output resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutput) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, monadResp, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdGet(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read output, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	config, err := connectorConfigToTF(output.Config.Settings, output.Config.Secrets)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to convert output config",
			fmt.Sprintf("Error converting config: %s", err),
		)
		return
	}

	description := types.StringNull()
	if output.Description != nil && *output.Description != "" {
		description = types.StringValue(*output.Description)
	}

	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = description
	data.ComponentType = types.StringValue(*output.Type)
	data.Config = config

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutput) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, secrets, err := data.getSettingsAndSecrets()
	if err != nil {
		resp.Diagnostics.AddError("Failed to get settings and secrets", err.Error())
		return
	}

	request := monad.RoutesV2PutOutputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		OutputType:  data.ComponentType.ValueStringPointer(),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	output, monadResp, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsOutputIdPut(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		RoutesV2PutOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update output, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	config, err := connectorConfigToTF(output.Config.Settings, output.Config.Secrets)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to convert output config",
			fmt.Sprintf("Error converting config: %s", err),
		)
		return
	}

	description := types.StringNull()
	if output.Description != nil {
		description = types.StringValue(*output.Description)
	}

	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = description
	data.ComponentType = types.StringValue(*output.Type)
	data.Config = config

	tflog.Trace(ctx, "updated a output resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutput) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdDelete(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete output, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}

func (r *ResourceOutput) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
