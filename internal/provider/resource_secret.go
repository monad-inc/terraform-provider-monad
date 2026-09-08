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

var _ resource.Resource = &ResourceSecret{}
var _ resource.ResourceWithConfigure = &ResourceSecret{}
var _ resource.ResourceWithImportState = &ResourceSecret{}
var _ resource.ResourceWithModifyPlan = &ResourceSecret{}

type ResourceSecret struct {
	client *client.Client
}

type ResourceSecretModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Value       types.String `tfsdk:"value"`
	ValueHash   types.String `tfsdk:"value_hash"`
}

func NewResourceSecret() resource.Resource {
	return &ResourceSecret{}
}

func (r *ResourceSecret) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *ResourceSecret) Configure(
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

func (r *ResourceSecret) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Secret",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Secret identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the secret",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the secret",
				Optional:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "Value of the secret. Write-only: sent to the Monad API " +
					"but never stored in Terraform state.",
				Required:  true,
				Sensitive: true,
				WriteOnly: true,
			},
			"value_hash": schema.StringAttribute{
				MarkdownDescription: "HMAC fingerprint of `value`, used to detect a rotated " +
					"secret. Changing `value` marks this unknown at plan and sends the " +
					"new value on apply.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ResourceSecret) computeValueHash(ctx context.Context, value string) string {
	return hmacSHA256Hex(ctx, secretsHashKey(r.client.OrganizationID), value)
}

func (r *ResourceSecret) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourceSecretModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request := monad.RoutesV2CreateOrUpdateSecretRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Value:       data.Value.ValueStringPointer(),
	}

	secret, monadResp, err := r.client.SecretsAPI.
		CreateSecret(ctx, r.client.OrganizationID).
		CreateSecretRequest(monad.RoutesV2CreateOrUpdateSecretRequestAsCreateSecretRequest(&request)).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create secret, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Only the computed id is taken from the response; name/description stay
	// as planned (apply consistency, see CLAUDE.md). The write-only value is
	// fingerprinted so a later rotation is detectable (ModifyPlan).
	data.ID = types.StringValue(*secret.Id)
	data.ValueHash = types.StringValue(r.computeValueHash(ctx, data.Value.ValueString()))

	tflog.Trace(ctx, "created a secret resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceSecret) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceSecretModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secret, monadResp, err := r.client.SecretsAPI.
		GetSecret(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			// Deleted outside Terraform — drop from state so the next plan
			// recreates it instead of erroring on refresh (ENG-9259).
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read secret, got error: %s. Response: %s",
				err,
				body,
			),
		)
		return
	}

	data.ID = types.StringValue(*secret.Id)
	data.Name = types.StringValue(*secret.Name)
	// The API echoes an unset description as ""; an omitted attribute is null
	// in config. Storing "" here produced a spurious `"" -> null` update on the
	// next plan and then an inconsistent-result error on apply (ENG-9867).
	data.Description = reconcileOptionalString(data.Description, secret.GetDescription())

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceSecret) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourceSecretModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// `value` is write-only, so it is null in the plan; the configuration is
	// the only place it is available. Reading it from the plan sent an empty
	// value, which the API treats as "keep the current ciphertext" — so a
	// value-only change never reached Monad (ENG-9235).
	var value types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value"), &value)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API preserves an omitted description on PATCH, so a description
	// removed from the configuration (null) must be sent as "" to clear it —
	// otherwise the server keeps the old text and every later plan re-diffs it.
	description := data.Description.ValueString()
	request := monad.RoutesV2CreateOrUpdateSecretRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: &description,
		Value:       value.ValueStringPointer(),
	}

	secret, monadResp, err := r.client.SecretsAPI.
		UpdateSecret(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		CreateSecretRequest(monad.RoutesV2CreateOrUpdateSecretRequestAsCreateSecretRequest(&request)).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update secret, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Preserve plan-known name/description; only the computed id comes from
	// the response. Echoing the API's description back ("" for an unset one)
	// is what tripped "produced inconsistent result after apply" (ENG-9867).
	data.ID = types.StringValue(*secret.Id)
	data.ValueHash = types.StringValue(r.computeValueHash(ctx, value.ValueString()))

	tflog.Trace(ctx, "updated a secret resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ModifyPlan detects a rotated `value`. Write-only values are null in state
// and in the plan, so a change to `value` alone produces no diff and Update
// would never run (ENG-9235). Mirroring modifyConnectorPlanForSecrets, it
// compares a fresh fingerprint of the configured value against the stored
// `value_hash` and, on a mismatch, marks `value_hash` unknown — which both
// triggers Update and lets Update store the new hash without an
// apply-consistency violation (an unknown planned value accepts any final
// value).
func (r *ResourceSecret) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if r.client == nil {
		return
	}
	// No prior state means a create; the hash is computed in Create.
	if req.State.Raw.IsNull() {
		return
	}
	// A planned destroy has a null plan; nothing to reconcile.
	if req.Plan.Raw.IsNull() {
		return
	}

	hashPath := path.Root("value_hash")

	var value types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value"), &value)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A value not known until apply (e.g. derived from another resource) may
	// or may not be a rotation; plan the update so Update can decide.
	if value.IsUnknown() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, hashPath, types.StringUnknown())...)
		return
	}

	var stateHash types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, hashPath, &stateHash)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if r.computeValueHash(ctx, value.ValueString()) != stateHash.ValueString() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, hashPath, types.StringUnknown())...)
	}
}

func (r *ResourceSecret) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceSecretModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monadResp, err := r.client.SecretsAPI.
		DeleteSecret(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete secret, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}

func (r *ResourceSecret) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
