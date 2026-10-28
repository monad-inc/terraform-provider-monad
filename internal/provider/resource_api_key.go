package provider

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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

var _ resource.Resource = &ResourceAPIKey{}
var _ resource.ResourceWithConfigure = &ResourceAPIKey{}
var _ resource.ResourceWithImportState = &ResourceAPIKey{}
var _ resource.ResourceWithModifyPlan = &ResourceAPIKey{}
var _ resource.ResourceWithValidateConfig = &ResourceAPIKey{}

// ResourceAPIKey manages an organization API key (PRO-713).
//
// The API returns the key's token only from create and regenerate; GET never
// echoes it. So, unlike `monad_secret`, the token is a Sensitive Computed
// attribute held in state: it is the only way to hand the token to anything
// else in the configuration. Changing `expiration_time` regenerates the key in
// place (same id, new token), because the API's update endpoint can change only
// name and description; changing `role_id` replaces it.
type ResourceAPIKey struct {
	client *client.Client
}

type ResourceAPIKeyModel struct {
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
	CreatedAt      types.String   `tfsdk:"created_at"`
	UpdatedAt      types.String   `tfsdk:"updated_at"`
	ID             types.String   `tfsdk:"id"`
	Name           types.String   `tfsdk:"name"`
	Description    types.String   `tfsdk:"description"`
	RoleID         types.String   `tfsdk:"role_id"`
	ExpirationTime types.String   `tfsdk:"expiration_time"`
	Token          types.String   `tfsdk:"token"`
}

func NewResourceAPIKey() resource.Resource {
	return &ResourceAPIKey{}
}

func (r *ResourceAPIKey) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *ResourceAPIKey) Configure(
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

func (r *ResourceAPIKey) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true, Read: true, Update: true, Delete: true,
			}),
		},
		MarkdownDescription: "Monad API Key",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API key identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API key",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the API key",
				Optional:            true,
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "ID of the organization role the key acts as. The API cannot " +
					"change a key's role, so changing it replaces the key (new id and token).",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"expiration_time": schema.StringAttribute{
				MarkdownDescription: "When the key expires, as an RFC 3339 timestamp " +
					"(`2027-01-01T00:00:00Z`). Must be in the future. Changing it regenerates the " +
					"key in place: the id stays, the token changes and the old one stops working.",
				Required: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "The key's token, for the `x-api-key` header. Returned by the " +
					"API only when the key is created or regenerated, so it is **stored in state** " +
					"(sensitive). Null after `terraform import`.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
	// The API key model has created_at and updated_at but no managed_by.
	withComputedMeta(resp.Schema.Attributes, false)
}

// ValidateConfig checks that expiration_time parses. Whether it is in the
// future is checked in ModifyPlan, only when the value is being sent: a key
// that has since expired must still plan (and destroy) cleanly.
func (r *ResourceAPIKey) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var data ResourceAPIKeyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.ExpirationTime.IsNull() || data.ExpirationTime.IsUnknown() {
		return
	}
	if _, err := time.Parse(time.RFC3339, data.ExpirationTime.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("expiration_time"),
			"Invalid expiration_time",
			fmt.Sprintf("expiration_time must be an RFC 3339 timestamp such as \"2027-01-01T00:00:00Z\": %s", err),
		)
	}
}

// ModifyPlan marks the token unknown when the key will be regenerated, and
// rejects an expiration_time in the past when it is about to be sent.
func (r *ResourceAPIKey) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}

	var plan ResourceAPIKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	creating := req.State.Raw.IsNull()

	// An expiration_time known only at apply (e.g. from time_rotating) may
	// differ from state, and then Update regenerates the key. Mark the token
	// unknown now: Terraform re-plans at apply once the value is known, and
	// a token that turns unknown only then fails as an inconsistent final
	// plan, so the key could never rotate this way.
	if plan.ExpirationTime.IsUnknown() {
		if !creating {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("token"), types.StringUnknown())...)
		}
		return
	}

	var state ResourceAPIKeyModel
	if !creating {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	expirationChanges := creating || !sameInstant(state.ExpirationTime, plan.ExpirationTime)
	if !expirationChanges {
		return
	}

	if exp, err := time.Parse(time.RFC3339, plan.ExpirationTime.ValueString()); err == nil && !exp.After(time.Now()) {
		resp.Diagnostics.AddAttributeError(
			path.Root("expiration_time"),
			"expiration_time is in the past",
			"The API rejects a key whose expiration_time is not in the future. Set a later time.",
		)
		return
	}

	// A replacement (role_id change) is planned as create, where the token is
	// already unknown. For an in-place expiration change, the key is
	// regenerated and gets a new token.
	if !creating {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("token"), types.StringUnknown())...)
	}
}

func (r *ResourceAPIKey) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourceAPIKeyModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The adopt-after-timeout lookup must outlive the create timeout, so it
	// uses the parent context (request_timeout still bounds each request).
	parentCtx := ctx
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Create, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	request := monad.RoutesV2CreateAPIKeyRequest{
		Name:           data.Name.ValueString(),
		Description:    data.Description.ValueStringPointer(),
		RoleId:         data.RoleID.ValueString(),
		ExpirationTime: data.ExpirationTime.ValueString(),
	}

	startedAt := time.Now().UTC()
	key, monadResp, err := r.client.OrganizationAPIKeysAPI.
		CreateAPIKey(ctx, r.client.OrganizationID).
		CreateAPIKeyRequest(monad.RoutesV2CreateAPIKeyRequestAsCreateAPIKeyRequest(&request)).
		Execute()
	createdID, ok := createOrAdopt(parentCtx, &resp.Diagnostics, adoptTarget{
		Kind:      "API key",
		Name:      data.Name.ValueString(),
		StartedAt: startedAt,
		List:      r.listForAdopt,
	}, key.GetId(), err, monadResp)
	if !ok {
		return
	}

	// Only computed values come from the response: the id, and the token.
	data.ID = types.StringValue(createdID)

	if err == nil {
		data.Token = types.StringValue(key.GetToken())
		data.CreatedAt = metaString(key.GetCreatedAt())
		data.UpdatedAt = metaString(key.GetUpdatedAt())
		tflog.Trace(ctx, "created an API key resource")
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}

	// Adopted after a timeout: the key exists but its token was in the response
	// that never arrived, and the API will not return it again. Nobody holds
	// that token, so regenerating (keeping the expiration) is safe and is the
	// only way to give state a usable one.
	regenerated, regenErr := r.regenerate(parentCtx, createdID, nil)
	if regenErr != nil {
		data.Token = types.StringNull()
		data.CreatedAt = types.StringNull()
		data.UpdatedAt = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		resp.Diagnostics.AddError(
			"API key adopted without a token",
			fmt.Sprintf(
				"API key %q (%s) was created after the request timed out, but regenerating it to "+
					"obtain a token failed: %s. The key is in state and will be replaced on the next apply.",
				data.Name.ValueString(), createdID, regenErr,
			),
		)
		return
	}
	data.Token = types.StringValue(regenerated.GetToken())
	data.CreatedAt = metaString(regenerated.GetCreatedAt())
	data.UpdatedAt = metaString(regenerated.GetUpdatedAt())
	resp.Diagnostics.AddWarning(
		"API key regenerated after adoption",
		fmt.Sprintf(
			"API key %q (%s) was adopted after a create timeout. Its original token was never "+
				"received, so the key was regenerated to obtain the token now in state.",
			data.Name.ValueString(), createdID,
		),
	)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceAPIKey) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceAPIKeyModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Read, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	key, monadResp, err := r.client.OrganizationAPIKeysAPI.
		GetAPIKey(ctx, r.client.OrganizationID, data.ID.ValueString()).
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
			fmt.Sprintf("Unable to read API key, got error: %s. Response: %s", err, body),
		)
		return
	}

	data.ID = types.StringValue(key.GetId())
	data.Name = types.StringValue(key.GetName())
	data.Description = reconcileOptionalString(data.Description, key.GetDescription())
	data.RoleID = types.StringValue(key.GetRoleId())
	data.ExpirationTime = reconcileTimestamp(data.ExpirationTime, key.GetExpirationTime())
	data.CreatedAt = metaString(key.GetCreatedAt())
	data.UpdatedAt = metaString(key.GetUpdatedAt())
	// The token is never returned by GET; keep whatever state holds (null after
	// import).

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceAPIKey) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan, state ResourceAPIKeyModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := withOperationTimeout(ctx, plan.Timeouts.Update, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	// updated_at is planned unknown on any change; it keeps the state value
	// unless a request below writes the key.
	plan.CreatedAt = state.CreatedAt
	plan.UpdatedAt = state.UpdatedAt

	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) {
		// The API treats an omitted field as "keep", so a description removed
		// from the configuration is sent as "" to clear it.
		description := plan.Description.ValueString()
		request := monad.RoutesV2UpdateAPIKeyRequest{
			Name:        plan.Name.ValueStringPointer(),
			Description: &description,
		}
		updated, monadResp, err := r.client.OrganizationAPIKeysAPI.
			UpdateAPIKey(ctx, r.client.OrganizationID, plan.ID.ValueString()).
			UpdateAPIKeyRequest(monad.RoutesV2UpdateAPIKeyRequestAsUpdateAPIKeyRequest(&request)).
			Execute()
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to update API key, got error: %s. Response: %s", err, getResponseBody(monadResp)),
			)
			return
		}
		plan.CreatedAt = metaString(updated.GetCreatedAt())
		plan.UpdatedAt = metaString(updated.GetUpdatedAt())
	}

	if sameInstant(state.ExpirationTime, plan.ExpirationTime) {
		// Only the timestamp's spelling changed (or nothing did); the key and
		// its token are unchanged.
		plan.Token = state.Token
	} else {
		expiration := plan.ExpirationTime.ValueString()
		regenerated, err := r.regenerate(ctx, plan.ID.ValueString(), &expiration)
		if err != nil {
			resp.Diagnostics.AddError("Unable to regenerate API key", err.Error())
			return
		}
		plan.Token = types.StringValue(regenerated.GetToken())
		plan.CreatedAt = metaString(regenerated.GetCreatedAt())
		plan.UpdatedAt = metaString(regenerated.GetUpdatedAt())
	}

	tflog.Trace(ctx, "updated an API key resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceAPIKey) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceAPIKeyModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Delete, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.OrganizationAPIKeysAPI.
		DeleteAPIKey(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			// Already gone remotely — nothing to delete.
			return
		}
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to delete API key, got error: %s. Response: %s", err, body),
		)
	}
}

func (r *ResourceAPIKey) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// regenerate rotates the key's secret and returns the regenerated key, which
// carries the new token. A nil expiration keeps the key's current one.
func (r *ResourceAPIKey) regenerate(ctx context.Context, id string, expiration *string) (*monad.ModelsAPIKeyWithToken, error) {
	request := monad.RoutesV2RegenerateAPIKeyRequest{ExpirationTime: expiration}
	key, monadResp, err := r.client.OrganizationAPIKeysAPI.
		RegenerateAPIKey(ctx, r.client.OrganizationID, id).
		RegenerateAPIKeyRequest(monad.RoutesV2RegenerateAPIKeyRequestAsRegenerateAPIKeyRequest(&request)).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("%s. Response: %s", err, getResponseBody(monadResp))
	}
	return key, nil
}

// listForAdopt is the adoptLister for API keys.
func (r *ResourceAPIKey) listForAdopt(ctx context.Context, limit, offset int32) ([]adoptCandidate, *int32, *http.Response, error) {
	page, httpResp, err := r.client.OrganizationAPIKeysAPI.ListAPIKeys(ctx, r.client.OrganizationID).
		Limit(limit).Offset(offset).Execute()
	if err != nil || page == nil {
		return nil, nil, httpResp, err
	}
	out := make([]adoptCandidate, 0, len(page.ApiKeys))
	for _, k := range page.ApiKeys {
		out = append(out, adoptCandidate{ID: k.Id, Name: k.Name, CreatedAt: k.CreatedAt})
	}
	total, _ := page.Pagination.GetTotalOk()
	return out, total, httpResp, nil
}

// sameInstant reports whether two RFC 3339 strings name the same moment. A
// value that does not parse compares by its text.
func sameInstant(a, b types.String) bool {
	if a.IsNull() || a.IsUnknown() || b.IsNull() || b.IsUnknown() {
		return a.Equal(b)
	}
	ta, errA := time.Parse(time.RFC3339, a.ValueString())
	tb, errB := time.Parse(time.RFC3339, b.ValueString())
	if errA != nil || errB != nil {
		return a.ValueString() == b.ValueString()
	}
	return ta.Equal(tb)
}

// reconcileTimestamp keeps the practitioner's spelling of a timestamp when the
// API reports the same instant in another form (it returns UTC), so an offset
// or a dropped fractional second never reads as drift.
func reconcileTimestamp(prior types.String, api string) types.String {
	apiValue := types.StringValue(api)
	if sameInstant(prior, apiValue) {
		return prior
	}
	return apiValue
}
