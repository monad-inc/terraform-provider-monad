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

var _ resource.Resource = &ResourceEnrichment{}

func NewResourceEnrichment() resource.Resource {
	return &ResourceEnrichment{}
}

type ResourceEnrichment struct {
	client *client.Client
}

func (r *ResourceEnrichment) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = fmt.Sprintf("%s_enrichment", req.ProviderTypeName)
}

func (r *ResourceEnrichment) Configure(
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

func (r *ResourceEnrichment) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = getConnectorSchema()
}

func (r *ResourceEnrichment) Create(
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

	request := monad.RoutesV3CreateEnrichmentRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Type:        data.ComponentType.ValueStringPointer(),
		Config: &monad.SecretProcessesorEnrichmentConfig{
			Settings: &monad.SecretProcessesorEnrichmentConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorEnrichmentConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	enrichment, monadResp, err := r.client.OrganizationEnrichmentsAPI.
		V3OrganizationIdEnrichmentsPost(ctx, r.client.OrganizationID).
		RoutesV3CreateEnrichmentRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create enrichment, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.ID = types.StringValue(*enrichment.Id)

	tflog.Trace(ctx, "created an enrichment resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceEnrichment) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	enrichment, monadResp, err := r.client.OrganizationEnrichmentsAPI.
		V3OrganizationIdEnrichmentsEnrichmentIdGet(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read enrichment, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.ID = types.StringValue(*enrichment.Id)
	data.Name = types.StringValue(*enrichment.Name)
	data.Description = types.StringValue(*enrichment.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceEnrichment) Update(
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

	request := monad.RoutesV3UpdateEnrichmentRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Type:        data.ComponentType.ValueStringPointer(),
		Config: &monad.SecretProcessesorEnrichmentConfig{
			Settings: &monad.SecretProcessesorEnrichmentConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorEnrichmentConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	enrichment, monadResp, err := r.client.OrganizationEnrichmentsAPI.
		V3OrganizationIdEnrichmentsEnrichmentIdPatch(ctx, r.client.OrganizationID, data.ID.ValueString()).
		RoutesV3UpdateEnrichmentRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update enrichment, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	data.ID = types.StringValue(*enrichment.Id)
	data.Name = types.StringValue(*enrichment.Name)
	data.Description = types.StringValue(*enrichment.Description)

	tflog.Trace(ctx, "updated an enrichment resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceEnrichment) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationEnrichmentsAPI.
		V3OrganizationIdEnrichmentsEnrichmentIdDelete(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete enrichment, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	tflog.Trace(ctx, "deleted an enrichment resource")
}
