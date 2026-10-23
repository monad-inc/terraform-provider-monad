package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourceSSOConnection{}
var _ resource.ResourceWithConfigure = &ResourceSSOConnection{}
var _ resource.ResourceWithImportState = &ResourceSSOConnection{}
var _ resource.ResourceWithValidateConfig = &ResourceSSOConnection{}

// ResourceSSOConnection manages a SAML SSO connection (PRO-716).
//
// API behaviors that shape it (api v0.281.1, pkg/routes/v3/routes/connections.go):
//   - Only name, description, public_name, session_settings and
//     saml.metadata_url can be updated. email_domains and saml.entity_id are
//     set at create only. A change to either fails the plan
//     (immutableNoReplace) rather than replacing the connection: deleting a
//     connection cascades to the organization's users, roles and sessions.
//   - PATCH writes name and description unconditionally (an omitted one is
//     blanked), so Update always sends both.
//   - public_name and session_settings are COALESCEd on update: they can be
//     changed but never cleared, so removing them from config keeps the
//     current value (Optional+Computed with UseStateForUnknown).
//   - The API normalizes email domains (lowercase, no "@" or scheme). Config
//     must already be in that form so plans stay clean.
//   - No field is secret: the IdP's certificate comes from its metadata URL.
//   - saml.entity_id is Required rather than generated: a generated one would
//     change if the connection were ever recreated, silently breaking the
//     IdP's audience setting.
type ResourceSSOConnection struct {
	client *client.Client
}

type ResourceSSOConnectionModel struct {
	Timeouts              timeouts.Value          `tfsdk:"timeouts"`
	ID                    types.String            `tfsdk:"id"`
	Name                  types.String            `tfsdk:"name"`
	Description           types.String            `tfsdk:"description"`
	EmailDomains          types.Set               `tfsdk:"email_domains"`
	PublicName            types.String            `tfsdk:"public_name"`
	SessionTimeoutMinutes types.Int64             `tfsdk:"session_timeout_minutes"`
	SAML                  *SSOConnectionSAMLModel `tfsdk:"saml"`
}

type SSOConnectionSAMLModel struct {
	EntityID    types.String `tfsdk:"entity_id"`
	MetadataURL types.String `tfsdk:"metadata_url"`
}

// Limits from the API's connection_validation.go and core models.
const (
	ssoPublicNameMaxLen     = 64
	ssoSessionTimeoutMinMin = 30
	ssoSessionTimeoutMaxMin = 1440
)

// ssoNoReplaceReason explains why a create-only change is refused rather than
// planned as a replacement.
const ssoNoReplaceReason = "Recreating an SSO connection deletes every user role, auth provider and session bound to it, " +
	"locking users out of the organization. The API cannot change this value in place, so restore the configured value to match the existing connection."

var ssoHostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// normalizeSSOEmailDomain mirrors the API's normalizeEmailDomain.
func normalizeSSOEmailDomain(raw string) string {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimPrefix(d, "@")
	d = strings.TrimSuffix(d, "/")
	return d
}

func NewResourceSSOConnection() resource.Resource {
	return &ResourceSSOConnection{}
}

func (r *ResourceSSOConnection) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_connection"
}

func (r *ResourceSSOConnection) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourceSSOConnection) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad SAML SSO connection",
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the connection",
				PlanModifiers:       keep,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connection",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the connection",
				Optional:            true,
			},
			"email_domains": schema.SetAttribute{
				MarkdownDescription: "Email domains whose users are sent to this connection at sign-in, in lowercase without `@`. Set at create only; the API cannot update it, so a change on an existing connection fails the plan.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.Set{immutableNoReplace{reason: ssoNoReplaceReason}},
			},
			"public_name": schema.StringAttribute{
				MarkdownDescription: "Label shown to users in the SSO sign-in picker. The API generates one (`sso-<hex>`) when omitted. Can be changed but not cleared.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keep,
			},
			"session_timeout_minutes": schema.Int64Attribute{
				MarkdownDescription: "Session length, in minutes, for sign-ins through this connection (30 to 1440). Unset uses the platform default. Can be changed but not cleared.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"saml": schema.SingleNestedAttribute{
				MarkdownDescription: "SAML settings",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"entity_id": schema.StringAttribute{
						MarkdownDescription: "Service provider entity ID (a UUID) that the identity provider sends as the assertion audience. Unique across Monad. Set at create only; a change on an existing connection fails the plan.",
						Required:            true,
						PlanModifiers:       []planmodifier.String{immutableNoReplace{reason: ssoNoReplaceReason}},
					},
					"metadata_url": schema.StringAttribute{
						MarkdownDescription: "HTTPS URL of the identity provider's SAML metadata. Monad fetches and validates it on create and on change.",
						Required:            true,
					},
				},
			},
		},
	}
}

// ValidateConfig applies the API's field rules at plan time.
func (r *ResourceSSOConnection) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ResourceSSOConnectionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.Name.IsNull() && !data.Name.IsUnknown() && strings.TrimSpace(data.Name.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Empty name", "name must not be empty.")
	}

	if !data.EmailDomains.IsNull() && !data.EmailDomains.IsUnknown() {
		var domains []types.String
		resp.Diagnostics.Append(data.EmailDomains.ElementsAs(ctx, &domains, false)...)
		for _, d := range domains {
			if d.IsUnknown() {
				continue
			}
			raw := d.ValueString()
			norm := normalizeSSOEmailDomain(raw)
			switch {
			case !ssoHostnameRe.MatchString(norm):
				resp.Diagnostics.AddAttributeError(path.Root("email_domains"), "Invalid email domain",
					fmt.Sprintf("%q is not a valid domain.", raw))
			case norm != raw:
				resp.Diagnostics.AddAttributeError(path.Root("email_domains"), "Email domain not in normalized form",
					fmt.Sprintf("The API stores %q as %q; use %q so plans stay clean.", raw, norm, norm))
			}
		}
	}

	if !data.PublicName.IsNull() && !data.PublicName.IsUnknown() {
		v := data.PublicName.ValueString()
		switch {
		case v == "":
			resp.Diagnostics.AddAttributeError(path.Root("public_name"), "Empty public_name",
				"public_name must not be empty; omit it to use the generated name.")
		case strings.TrimSpace(v) != v:
			resp.Diagnostics.AddAttributeError(path.Root("public_name"), "Invalid public_name",
				"public_name must not start or end with whitespace; the API trims it.")
		case len(v) > ssoPublicNameMaxLen:
			resp.Diagnostics.AddAttributeError(path.Root("public_name"), "Invalid public_name",
				fmt.Sprintf("public_name must be %d characters or fewer.", ssoPublicNameMaxLen))
		case strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
			resp.Diagnostics.AddAttributeError(path.Root("public_name"), "Invalid public_name",
				"public_name must not contain control characters.")
		}
	}

	if !data.SessionTimeoutMinutes.IsNull() && !data.SessionTimeoutMinutes.IsUnknown() {
		if v := data.SessionTimeoutMinutes.ValueInt64(); v < ssoSessionTimeoutMinMin || v > ssoSessionTimeoutMaxMin {
			resp.Diagnostics.AddAttributeError(path.Root("session_timeout_minutes"), "Invalid session_timeout_minutes",
				fmt.Sprintf("session_timeout_minutes must be between %d and %d.", ssoSessionTimeoutMinMin, ssoSessionTimeoutMaxMin))
		}
	}

	if data.SAML != nil {
		if e := data.SAML.EntityID; !e.IsNull() && !e.IsUnknown() {
			if _, err := uuid.ParseUUID(e.ValueString()); err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("saml").AtName("entity_id"), "Invalid entity_id",
					fmt.Sprintf("%q is not a UUID.", e.ValueString()))
			}
		}
		if u := data.SAML.MetadataURL; !u.IsNull() && !u.IsUnknown() && !strings.HasPrefix(u.ValueString(), "https://") {
			resp.Diagnostics.AddAttributeError(path.Root("saml").AtName("metadata_url"), "Invalid metadata_url",
				"metadata_url must use https.")
		}
	}
}

func (r *ResourceSSOConnection) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ResourceSSOConnectionModel
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

	request := monad.RoutesV3CreateConnectionRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Saml: &monad.RoutesV3CreateConnectionRequestSaml{
			EntityId:    data.SAML.EntityID.ValueStringPointer(),
			MetadataUrl: data.SAML.MetadataURL.ValueStringPointer(),
		},
	}
	if !data.EmailDomains.IsNull() {
		resp.Diagnostics.Append(data.EmailDomains.ElementsAs(ctx, &request.EmailDomains, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if !data.PublicName.IsUnknown() && !data.PublicName.IsNull() {
		request.PublicName = data.PublicName.ValueStringPointer()
	}
	if s := ssoSessionSettings(data.SessionTimeoutMinutes); s != nil {
		request.SessionSettings = s
	}

	startedAt := time.Now().UTC()
	conn, monadResp, err := r.client.ConnectionsAPI.
		CreateConnection(ctx, r.client.OrganizationID).
		CreateConnectionRequest(monad.RoutesV3CreateConnectionRequestAsCreateConnectionRequest(&request)).
		Execute()
	// entity_id is unique across Monad, so it identifies this create's
	// connection exactly; it stands in for the name in the adopt match.
	createdID, ok := createOrAdopt(parentCtx, &resp.Diagnostics, adoptTarget{
		Kind:      "SSO connection",
		Name:      data.SAML.EntityID.ValueString(),
		StartedAt: startedAt,
		List:      r.listForAdopt,
	}, conn.GetId(), err, monadResp)
	if !ok {
		return
	}

	if err != nil {
		// Adopted after a timeout: read the computed fields back.
		conn, monadResp, err = r.client.ConnectionsAPI.GetConnectionByID(parentCtx, r.client.OrganizationID, createdID).Execute()
		if err != nil {
			resp.Diagnostics.AddError("Unable to read adopted SSO connection",
				fmt.Sprintf("%s. Response: %s", err, getResponseBody(monadResp)))
			return
		}
	}
	data.ID = types.StringValue(createdID)
	setSSOComputed(&data, conn)

	tflog.Trace(ctx, "created an SSO connection")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// setSSOComputed fills the values the API decided: those the plan left
// unknown because config omitted them.
func setSSOComputed(data *ResourceSSOConnectionModel, conn *monad.ModelsConnection) {
	if data.PublicName.IsUnknown() {
		data.PublicName = types.StringValue(conn.GetPublicName())
	}
	if data.SessionTimeoutMinutes.IsUnknown() {
		data.SessionTimeoutMinutes = ssoSessionTimeout(conn)
	}
}

func ssoSessionSettings(v types.Int64) *monad.ModelsConnectionSessionSettings {
	if v.IsUnknown() || v.IsNull() {
		return nil
	}
	minutes := int32(v.ValueInt64())
	return &monad.ModelsConnectionSessionSettings{SessionTimeout: &minutes}
}

func ssoSessionTimeout(conn *monad.ModelsConnection) types.Int64 {
	if s, ok := conn.GetSessionSettingsOk(); ok && s.SessionTimeout != nil {
		return types.Int64Value(int64(*s.SessionTimeout))
	}
	return types.Int64Null()
}

func (r *ResourceSSOConnection) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// No client: the provider configuration is still unknown. Keep the prior
	// state until it is known.
	if r.client == nil {
		return
	}

	var data ResourceSSOConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Read, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	conn, monadResp, err := r.client.ConnectionsAPI.
		GetConnectionByID(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to read SSO connection: %s. Response: %s", err, body))
		return
	}

	data.ID = types.StringValue(conn.GetId())
	data.Name = types.StringValue(conn.GetName())
	data.Description = reconcileOptionalString(data.Description, conn.GetDescription())
	if domains := conn.GetEmailDomains(); len(domains) > 0 || !data.EmailDomains.IsNull() {
		set, diags := types.SetValueFrom(ctx, types.StringType, domains)
		resp.Diagnostics.Append(diags...)
		data.EmailDomains = set
	}
	data.PublicName = types.StringValue(conn.GetPublicName())
	data.SessionTimeoutMinutes = ssoSessionTimeout(conn)
	data.SAML = &SSOConnectionSAMLModel{
		EntityID:    types.StringValue(conn.GetSamlEntityId()),
		MetadataURL: types.StringValue(conn.GetSamlMetadataUrl()),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceSSOConnection) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ResourceSSOConnectionModel
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

	// The API overwrites name and description on every PATCH, so both are
	// always sent; a null description clears it.
	description := plan.Description.ValueString()
	request := monad.RoutesV3UpdateConnectionRequest{
		Name:            plan.Name.ValueStringPointer(),
		Description:     &description,
		SessionSettings: ssoSessionSettings(plan.SessionTimeoutMinutes),
	}
	if !plan.PublicName.IsUnknown() && !plan.PublicName.IsNull() {
		request.PublicName = plan.PublicName.ValueStringPointer()
	}
	// Only on change: the API re-fetches and validates the metadata each time.
	if !plan.SAML.MetadataURL.Equal(state.SAML.MetadataURL) {
		request.Saml = &monad.RoutesV3UpdateConnectionRequestSaml{MetadataUrl: plan.SAML.MetadataURL.ValueStringPointer()}
	}

	conn, monadResp, err := r.client.ConnectionsAPI.
		UpdateConnection(ctx, r.client.OrganizationID, plan.ID.ValueString()).
		UpdateConnectionRequest(monad.RoutesV3UpdateConnectionRequestAsUpdateConnectionRequest(&request)).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to update SSO connection: %s. Response: %s", err, getResponseBody(monadResp)))
		return
	}
	setSSOComputed(&plan, conn)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceSSOConnection) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ResourceSSOConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Delete, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	monadResp, err := r.client.ConnectionsAPI.
		DeleteConnection(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to delete SSO connection: %s. Response: %s", err, body))
	}
}

func (r *ResourceSSOConnection) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// listForAdopt reports each connection's entity_id as its name; see Create.
func (r *ResourceSSOConnection) listForAdopt(ctx context.Context, limit, offset int32) ([]adoptCandidate, *int32, *http.Response, error) {
	page, httpResp, err := r.client.ConnectionsAPI.ListConnections(ctx, r.client.OrganizationID).
		Limit(limit).Offset(offset).Execute()
	if err != nil || page == nil {
		return nil, nil, httpResp, err
	}
	out := make([]adoptCandidate, 0, len(page.Connections))
	for _, c := range page.Connections {
		out = append(out, adoptCandidate{ID: c.Id, Name: c.SamlEntityId, CreatedAt: c.CreatedAt})
	}
	total, _ := page.Pagination.GetTotalOk()
	return out, total, httpResp, nil
}
