package provider

import (
	"context"
	"fmt"

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

var _ resource.Resource = &ResourceEnrichment{}
var _ resource.ResourceWithConfigure = &ResourceEnrichment{}
var _ resource.ResourceWithImportState = &ResourceEnrichment{}
var _ resource.ResourceWithModifyPlan = &ResourceEnrichment{}

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
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Enrichment",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Enrichment identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the enrichment",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the enrichment",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of the enrichment",
				Required:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "Enrichment configuration",
				Attributes: map[string]schema.Attribute{
					"settings": schema.DynamicAttribute{
						MarkdownDescription: "Settings for the enrichment",
						Optional:            true,
					},
					"secrets": schema.DynamicAttribute{
						MarkdownDescription: "Secrets for the enrichment. Write-only: the " +
							"value is sent to the Monad API but never persisted in " +
							"Terraform state. Rotation is detected via `secrets_hash`.",
						Optional:  true,
						Sensitive: true,
						WriteOnly: true,
					},
					"secrets_hash": schema.StringAttribute{
						MarkdownDescription: "HMAC fingerprint of `secrets`, used to " +
							"detect when the write-only secret values change. Managed " +
							"by the provider.",
						Computed: true,
					},
				},
			},
		},
	}
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

	// Only the computed `id` is taken from the response; name/description/type/
	// settings are plan-known and must be returned unchanged (their planned cty
	// type must be preserved — rebuilding from the response trips "Provider
	// produced inconsistent result after apply"). Secrets are write-only, so
	// they are nulled in state and fingerprinted into secrets_hash.
	data.ID = types.StringValue(*enrichment.Id)
	if err := finalizeConnectorSecrets(ctx, r.client.OrganizationID, &data, secrets); err != nil {
		resp.Diagnostics.AddError("Failed to fingerprint enrichment secrets", err.Error())
		return
	}

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
		V3OrganizationIdEnrichmentsEnrichmentIdGet(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
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

	description := types.StringNull()
	if enrichment.Description != nil && *enrichment.Description != "" {
		description = types.StringValue(*enrichment.Description)
	}

	data.ID = types.StringValue(*enrichment.Id)
	data.Name = types.StringValue(*enrichment.Name)
	data.Description = description
	data.ComponentType = types.StringValue(*enrichment.Type)
	if err := refreshConnectorSettings(&data, enrichment.Config.Settings); err != nil {
		resp.Diagnostics.AddError("Failed to refresh enrichment settings", err.Error())
		return
	}

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

	request := monad.RoutesV3PutEnrichmentRequest{
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

	_, monadResp, err := r.client.OrganizationEnrichmentsAPI.
		V3OrganizationIdEnrichmentsEnrichmentIdPut(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		RoutesV3PutEnrichmentRequest(request).
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

	// Preserve plan-known values (see Create); data already holds
	// id/name/description/type/settings from the plan. Secrets stay write-only
	// (nulled) and secrets_hash is refreshed to match what was just sent.
	if err := finalizeConnectorSecrets(ctx, r.client.OrganizationID, &data, secrets); err != nil {
		resp.Diagnostics.AddError("Failed to fingerprint enrichment secrets", err.Error())
		return
	}

	tflog.Trace(ctx, "updated an enrichment resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceEnrichment) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if r.client == nil {
		return
	}
	modifyConnectorPlanForSecrets(ctx, r.client.OrganizationID, req, resp)
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
		).Execute()
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
}

func (r *ResourceEnrichment) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
