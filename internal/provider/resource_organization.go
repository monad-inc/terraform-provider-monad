package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourceOrganization{}
var _ resource.ResourceWithConfigure = &ResourceOrganization{}
var _ resource.ResourceWithImportState = &ResourceOrganization{}
var _ resource.ResourceWithValidateConfig = &ResourceOrganization{}
var _ resource.ResourceWithModifyPlan = &ResourceOrganization{}

// ResourceOrganization manages a sub-organization (child organization) of the
// provider's organization (PRO-715).
//
// Three API behaviors shape it:
//   - The API stores `name` with a random 4-hex suffix (`acme` becomes
//     `acme-161d`), so `name` is the requested prefix and `slug` is what the
//     API stored.
//   - Only friendly_name and description can change; a new name is a new
//     organization (RequiresReplace).
//   - Deleting a sub-org hard-deletes it and everything in it, so
//     `deletion_protection` defaults to true and makes Delete (and therefore
//     replacement) fail until it is set to false and applied.
type ResourceOrganization struct {
	client *client.Client
}

type ResourceOrganizationModel struct {
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
	ID                   types.String   `tfsdk:"id"`
	Name                 types.String   `tfsdk:"name"`
	Slug                 types.String   `tfsdk:"slug"`
	FriendlyName         types.String   `tfsdk:"friendly_name"`
	Description          types.String   `tfsdk:"description"`
	ParentOrganizationID types.String   `tfsdk:"parent_organization_id"`
	BillingAccountID     types.String   `tfsdk:"billing_account_id"`
	DeletionProtection   types.Bool     `tfsdk:"deletion_protection"`
}

// The API's organization name rules (ValidateOrganizationName): a lowercase
// DNS label of at most 22 characters, before the suffix is added.
const orgNameMaxLen = 22

var (
	orgNameRe   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	orgSuffixRe = regexp.MustCompile(`^(.+)-[0-9a-f]{4}$`)
)

// orgNamePrefix recovers the requested name from the stored slug.
func orgNamePrefix(slug string) string {
	if m := orgSuffixRe.FindStringSubmatch(slug); m != nil {
		return m[1]
	}
	return slug
}

func NewResourceOrganization() resource.Resource {
	return &ResourceOrganization{}
}

func (r *ResourceOrganization) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *ResourceOrganization) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourceOrganization) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Sub-organization",
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the sub-organization",
				PlanModifiers:       keep,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the sub-organization: lowercase letters, digits and `-`, at most 22 characters. The API appends a random suffix (see `slug`). Changing it creates a new sub-organization.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"slug": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The name as the API stored it, with its random suffix, such as `acme-161d`.",
				PlanModifiers:       keep,
			},
			"friendly_name": schema.StringAttribute{
				MarkdownDescription: "Display name. Defaults to `name`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keep,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the sub-organization",
				Optional:            true,
			},
			"parent_organization_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The parent organization: the provider's `organization_id`.",
				PlanModifiers:       keep,
			},
			"billing_account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Billing account, inherited from the parent.",
				PlanModifiers:       keep,
			},
			"deletion_protection": schema.BoolAttribute{
				MarkdownDescription: "While `true` (the default), a plan that would destroy or replace the sub-organization fails. Deleting a sub-organization deletes everything in it, so set this to `false` and apply before destroying.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateConfig applies the API's name rules at plan time.
func (r *ResourceOrganization) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ResourceOrganizationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.Name.IsNull() || data.Name.IsUnknown() {
		return
	}
	name := data.Name.ValueString()
	if len(name) > orgNameMaxLen || !orgNameRe.MatchString(name) {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid organization name",
			fmt.Sprintf("%q must be lowercase letters, digits and '-', start and end with a letter or digit, and be at most %d characters.", name, orgNameMaxLen))
	}
	if !data.FriendlyName.IsNull() && !data.FriendlyName.IsUnknown() && data.FriendlyName.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(path.Root("friendly_name"), "Empty friendly_name",
			"The API does not allow an empty friendly_name; omit it to use `name`.")
	}
}

func (r *ResourceOrganization) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ResourceOrganizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parentCtx := ctx
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Create, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	request := monad.RoutesV3CreateChildOrganizationRequest{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueStringPointer(),
	}
	if !data.FriendlyName.IsUnknown() && !data.FriendlyName.IsNull() {
		request.FriendlyName = data.FriendlyName.ValueStringPointer()
	}

	startedAt := time.Now().UTC()
	org, monadResp, err := r.client.OrganizationsAPI.
		CreateChildOrganization(ctx, r.client.OrganizationID).
		CreateChildOrganizationRequest(monad.RoutesV3CreateChildOrganizationRequestAsCreateChildOrganizationRequest(&request)).
		Execute()
	createdID, ok := createOrAdopt(parentCtx, &resp.Diagnostics, adoptTarget{
		Kind:      "sub-organization",
		Name:      data.Name.ValueString(),
		StartedAt: startedAt,
		List:      r.listForAdopt,
	}, org.GetId(), err, monadResp)
	if !ok {
		return
	}

	data.ID = types.StringValue(createdID)
	if err == nil {
		r.setComputed(&data, org.GetName(), org.GetFriendlyName(), org.GetParentOrganizationId(), org.GetBillingAccountId())
	} else {
		// Adopted after a timeout: read the computed fields back.
		found, err := r.find(parentCtx, createdID, data.Name.ValueString())
		if err != nil || found == nil {
			resp.Diagnostics.AddError("Unable to read adopted sub-organization", fmt.Sprintf("%v", err))
			return
		}
		r.setComputed(&data, found.GetName(), found.GetFriendlyName(), found.GetParentOrganizationId(), found.GetBillingAccountId())
	}

	tflog.Trace(ctx, "created a sub-organization")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// setComputed fills the attributes the API decides. friendly_name is only
// taken from the API when the plan left it unknown (omitted).
func (r *ResourceOrganization) setComputed(data *ResourceOrganizationModel, slug, friendlyName, parent, billing string) {
	data.Slug = types.StringValue(slug)
	if data.FriendlyName.IsUnknown() || data.FriendlyName.IsNull() {
		data.FriendlyName = types.StringValue(friendlyName)
	}
	data.ParentOrganizationID = types.StringValue(parent)
	data.BillingAccountID = metaStringOrEmpty(billing)
}

func metaStringOrEmpty(v string) types.String {
	if v == "" {
		return types.StringNull()
	}
	return types.StringValue(v)
}

func (r *ResourceOrganization) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// No client: the provider configuration is still unknown (see
	// MonadProvider.Configure). Keep the prior state until it is known.
	if r.client == nil {
		return
	}

	var data ResourceOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Read, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.find(ctx, data.ID.ValueString(), data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read sub-organization: %s", err))
		return
	}
	if org == nil {
		// Deleted outside Terraform, or no longer a child of this organization.
		resp.State.RemoveResource(ctx)
		return
	}

	data.ID = types.StringValue(org.GetId())
	if data.Name.IsNull() {
		// Import: recover the requested name from the stored slug.
		data.Name = types.StringValue(orgNamePrefix(org.GetName()))
	}
	data.Slug = types.StringValue(org.GetName())
	data.FriendlyName = types.StringValue(org.GetFriendlyName())
	data.Description = reconcileOptionalString(data.Description, org.GetDescription())
	data.ParentOrganizationID = types.StringValue(org.GetParentOrganizationId())
	data.BillingAccountID = metaStringOrEmpty(org.GetBillingAccountId())
	if data.DeletionProtection.IsNull() {
		data.DeletionProtection = types.BoolValue(true) // import: protected until said otherwise
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOrganization) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ResourceOrganizationModel
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

	// deletion_protection is provider-side only; send a request only when an
	// API field changed.
	if !plan.FriendlyName.Equal(state.FriendlyName) || !plan.Description.Equal(state.Description) {
		description := plan.Description.ValueString() // "" clears it; omitted means "keep" to the API
		request := monad.RoutesV3UpdateChildOrganizationRequest{Description: &description}
		if !plan.FriendlyName.IsUnknown() && !plan.FriendlyName.IsNull() {
			request.FriendlyName = plan.FriendlyName.ValueStringPointer()
		}
		org, monadResp, err := r.client.OrganizationsAPI.
			UpdateChildOrganization(ctx, r.client.OrganizationID, plan.ID.ValueString()).
			UpdateChildOrganizationRequest(monad.RoutesV3UpdateChildOrganizationRequestAsUpdateChildOrganizationRequest(&request)).
			Execute()
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to update sub-organization: %s. Response: %s", err, getResponseBody(monadResp)))
			return
		}
		if plan.FriendlyName.IsUnknown() || plan.FriendlyName.IsNull() {
			plan.FriendlyName = types.StringValue(org.GetFriendlyName())
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan refuses a destroy or replacement of a protected sub-organization at
// plan time. Checking only in Delete is too late: Terraform destroys resources
// that depend on the sub-organization (anything managed through a provider alias
// pointed at it) before it calls Delete here.
func (r *ResourceOrganization) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() {
		return
	}
	var state ResourceOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !state.DeletionProtection.ValueBool() {
		return
	}

	if req.Plan.Raw.IsNull() {
		addDeletionProtectedError(&resp.Diagnostics, &state)
		return
	}
	var planName types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &planName)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !planName.IsUnknown() && !planName.Equal(state.Name) {
		addDeletionProtectedError(&resp.Diagnostics, &state)
	}
}

func addDeletionProtectedError(diags *diag.Diagnostics, data *ResourceOrganizationModel) {
	diags.AddError("Sub-organization is deletion-protected",
		fmt.Sprintf(
			"Deleting %q (%s) would delete every pipeline, component, secret, key and user assignment in it. "+
				"To proceed, set deletion_protection = false, apply, then destroy (or change the name) again.",
			data.Slug.ValueString(), data.ID.ValueString(),
		))
}

func (r *ResourceOrganization) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ResourceOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Backstop in case a protected destroy reaches apply without ModifyPlan.
	if data.DeletionProtection.ValueBool() {
		addDeletionProtectedError(&resp.Diagnostics, &data)
		return
	}

	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Delete, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	monadResp, err := r.client.OrganizationsAPI.
		DeleteChildOrganization(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to delete sub-organization: %s. Response: %s", err, body))
	}
}

func (r *ResourceOrganization) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// find returns the sub-organization with the given ID among this
// organization's children, or nil. There is no get-by-ID endpoint, so it pages
// the children list, narrowed by name when the name is known.
func (r *ResourceOrganization) find(ctx context.Context, id, name string) (*monad.ModelsUserOrganization, error) {
	search := func(nameSearch string) (*monad.ModelsUserOrganization, error) {
		const pageSize = int32(100)
		for offset := int32(0); ; offset += pageSize {
			q := r.client.OrganizationsAPI.ListChildOrganizations(ctx, r.client.OrganizationID).Limit(pageSize).Offset(offset)
			if nameSearch != "" {
				q = q.NameSearch(nameSearch)
			}
			page, httpResp, err := q.Execute()
			if err != nil {
				return nil, fmt.Errorf("%s. Response: %s", err, getResponseBody(httpResp))
			}
			for i := range page.Organizations {
				if page.Organizations[i].GetId() == id {
					return &page.Organizations[i], nil
				}
			}
			total, ok := page.Pagination.GetTotalOk()
			if len(page.Organizations) == 0 || (ok && offset+int32(len(page.Organizations)) >= *total) {
				return nil, nil
			}
		}
	}
	if name != "" {
		if org, err := search(name); err != nil || org != nil {
			return org, err
		}
	}
	return search("")
}

// listForAdopt is the adoptLister for sub-organizations. Candidates carry the
// name prefix, not the stored slug, so they compare with the requested name.
func (r *ResourceOrganization) listForAdopt(ctx context.Context, limit, offset int32) ([]adoptCandidate, *int32, *http.Response, error) {
	page, httpResp, err := r.client.OrganizationsAPI.ListChildOrganizations(ctx, r.client.OrganizationID).
		Limit(limit).Offset(offset).Execute()
	if err != nil || page == nil {
		return nil, nil, httpResp, err
	}
	out := make([]adoptCandidate, 0, len(page.Organizations))
	for _, o := range page.Organizations {
		prefix := orgNamePrefix(o.GetName())
		out = append(out, adoptCandidate{ID: o.Id, Name: &prefix, CreatedAt: o.CreatedAt})
	}
	total, _ := page.Pagination.GetTotalOk()
	return out, total, httpResp, nil
}
