package provider

import (
	"context"
	"fmt"
	"sort"
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

var _ resource.Resource = &ResourceRole{}
var _ resource.ResourceWithConfigure = &ResourceRole{}
var _ resource.ResourceWithImportState = &ResourceRole{}
var _ resource.ResourceWithValidateConfig = &ResourceRole{}
var _ resource.ResourceWithModifyPlan = &ResourceRole{}

// ResourceRole manages a custom organization role (PRO-714). Permissions are
// written as slugs (`pipeline:read`) and resolved to the catalog's IDs on
// apply. The built-in roles are `protected`: the API refuses to update or
// delete them, so they can be looked up with the `monad_role` data source but
// not managed here.
type ResourceRole struct {
	client *client.Client
}

type ResourceRoleModel struct {
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
	CreatedAt   types.String   `tfsdk:"created_at"`
	UpdatedAt   types.String   `tfsdk:"updated_at"`
	ID          types.String   `tfsdk:"id"`
	Name        types.String   `tfsdk:"name"`
	Description types.String   `tfsdk:"description"`
	Permissions types.Set      `tfsdk:"permissions"`
}

func NewResourceRole() resource.Resource {
	return &ResourceRole{}
}

func (r *ResourceRole) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *ResourceRole) Configure(
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

func (r *ResourceRole) Schema(
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
		MarkdownDescription: "Monad Role",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Role identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the role",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the role",
				Optional:            true,
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "Permission slugs the role grants, such as `pipeline:read`. At least " +
					"one. See the `monad_permissions` data source for the catalog.",
				Required:    true,
				ElementType: types.StringType,
			},
		},
	}
	// The role model has created_at and updated_at but no managed_by.
	withComputedMeta(resp.Schema.Attributes, false)
}

// ValidateConfig requires at least one permission. The update endpoint treats
// an empty permission list as "keep the current ones", so a role could never
// be reduced to none; refusing it here keeps plan and result in agreement.
func (r *ResourceRole) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var data ResourceRoleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.Permissions.IsUnknown() || data.Permissions.IsNull() {
		return
	}
	if len(data.Permissions.Elements()) == 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("permissions"),
			"Role needs at least one permission",
			"The API cannot remove a role's last permission, so an empty set would never apply. "+
				"Grant at least one permission, or delete the role.",
		)
	}
}

// ModifyPlan rejects an unknown permission slug at plan time, so a typo never
// reaches apply. It reads the catalog only when the permissions are being sent
// (a create, or a changed set), so an unchanged role plans without the extra
// request.
func (r *ResourceRole) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return // destroy, or the provider is not configured yet
	}

	var plan ResourceRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Permissions.IsUnknown() || plan.Permissions.IsNull() {
		return
	}

	if !req.State.Raw.IsNull() {
		var state ResourceRoleModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if state.Permissions.Equal(plan.Permissions) {
			return
		}
	}

	// An unknown element (from another resource) cannot be checked yet; Create
	// and Update resolve the full set again at apply.
	for _, e := range plan.Permissions.Elements() {
		if e.IsUnknown() {
			return
		}
	}

	r.permissionIDs(ctx, plan.Permissions, &resp.Diagnostics)
}

func (r *ResourceRole) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourceRoleModel

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

	permissionIDs, ok := r.permissionIDs(ctx, data.Permissions, &resp.Diagnostics)
	if !ok {
		return
	}

	request := monad.RoutesV2CreateRoleV2Request{
		Name:          data.Name.ValueString(),
		Description:   data.Description.ValueStringPointer(),
		PermissionIds: permissionIDs,
	}

	startedAt := time.Now().UTC()
	role, monadResp, err := r.client.RolesAPI.
		CreateRole(ctx, r.client.OrganizationID).
		CreateRoleRequest(monad.RoutesV2CreateRoleV2RequestAsCreateRoleRequest(&request)).
		Execute()
	createdID, ok := createOrAdopt(parentCtx, &resp.Diagnostics, adoptTarget{
		Kind:      "role",
		Name:      data.Name.ValueString(),
		StartedAt: startedAt,
		List:      listRolesForAdopt(r.client),
	}, role.GetId(), err, monadResp)
	if !ok {
		return
	}

	// Only computed values are taken from the response (apply consistency).
	data.ID = types.StringValue(createdID)
	if err != nil {
		// Adopted after a timeout: the response never arrived, so read the
		// role for its timestamps. If that fails too, the next refresh fills
		// them in.
		role, _, err = r.client.RolesAPI.GetRole(parentCtx, r.client.OrganizationID, createdID).Execute()
	}
	data.CreatedAt = types.StringNull()
	data.UpdatedAt = types.StringNull()
	if err == nil {
		data.CreatedAt = metaString(role.GetCreatedAt())
		data.UpdatedAt = metaString(role.GetUpdatedAt())
	}

	tflog.Trace(ctx, "created a role resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceRole) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceRoleModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Read, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	role, monadResp, err := r.client.RolesAPI.
		GetRole(ctx, r.client.OrganizationID, data.ID.ValueString()).
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
			fmt.Sprintf("Unable to read role, got error: %s. Response: %s", err, body),
		)
		return
	}

	data.ID = types.StringValue(role.GetId())
	data.Name = types.StringValue(role.GetName())
	data.Description = reconcileOptionalString(data.Description, role.GetDescription())
	data.CreatedAt = metaString(role.GetCreatedAt())
	data.UpdatedAt = metaString(role.GetUpdatedAt())

	// A set, so the API's order never reads as drift.
	slugs := permissionSlugs(role.Permissions)
	sort.Strings(slugs)
	permissions, diags := types.SetValueFrom(ctx, types.StringType, slugs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Permissions = permissions

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceRole) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourceRoleModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Update, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	permissionIDs, ok := r.permissionIDs(ctx, data.Permissions, &resp.Diagnostics)
	if !ok {
		return
	}

	// An omitted field means "keep" to the API, so a description removed from
	// the configuration is sent as "" to clear it.
	description := data.Description.ValueString()
	request := monad.RoutesV2UpdateRoleV2Request{
		Name:          data.Name.ValueStringPointer(),
		Description:   &description,
		PermissionIds: permissionIDs,
	}

	updated, monadResp, err := r.client.RolesAPI.
		UpdateRole(ctx, r.client.OrganizationID, data.ID.ValueString()).
		UpdateRoleRequest(monad.RoutesV2UpdateRoleV2RequestAsUpdateRoleRequest(&request)).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to update role, got error: %s. Response: %s", err, getResponseBody(monadResp)),
		)
		return
	}
	data.CreatedAt = metaString(updated.GetCreatedAt())
	data.UpdatedAt = metaString(updated.GetUpdatedAt())

	tflog.Trace(ctx, "updated a role resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceRole) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceRoleModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Delete, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.RolesAPI.
		DeleteRole(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			// Already gone remotely — nothing to delete.
			return
		}
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete role, got error: %s. Response: %s. A role still assigned to a "+
					"user or an API key cannot be deleted; reassign or remove those first.",
				err, body,
			),
		)
	}
}

// ImportState refuses the built-in (protected) roles: the API rejects every
// update and delete of them, so managing one would fail on the first change and
// on destroy. They are referenced through the `monad_role` data source instead.
func (r *ResourceRole) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	if r.client != nil {
		role, monadResp, err := r.client.RolesAPI.GetRole(ctx, r.client.OrganizationID, req.ID).Execute()
		if err == nil && role.GetProtected() {
			resp.Diagnostics.AddError(
				"Cannot import a built-in role",
				fmt.Sprintf(
					"%q is a built-in role, which the API does not allow to be changed or deleted, so it "+
						"cannot be managed by Terraform. Reference it with the monad_role data source instead: "+
						"data \"monad_role\" \"x\" { name = %q }",
					role.GetName(), role.GetName(),
				),
			)
			return
		}
		if err != nil && !isNotFoundResponse(monadResp, getResponseBody(monadResp)) {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to read role %s for import: %s", req.ID, err),
			)
			return
		}
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// permissionIDs resolves the planned slugs to permission IDs.
func (r *ResourceRole) permissionIDs(ctx context.Context, set types.Set, diags interface {
	AddAttributeError(path.Path, string, string)
	AddError(string, string)
}) ([]string, bool) {
	var slugs []string
	if d := set.ElementsAs(ctx, &slugs, false); d.HasError() {
		diags.AddError("Invalid permissions", fmt.Sprintf("%v", d))
		return nil, false
	}
	catalog, err := listAllPermissions(ctx, r.client)
	if err != nil {
		diags.AddError("Unable to read the permission catalog", err.Error())
		return nil, false
	}
	ids, err := resolvePermissionSlugs(catalog, slugs)
	if err != nil {
		diags.AddAttributeError(path.Root("permissions"), "Unknown permission", err.Error())
		return nil, false
	}
	return ids, true
}
