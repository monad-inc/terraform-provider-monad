package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ datasource.DataSource = &DataSourceRole{}
var _ datasource.DataSourceWithConfigure = &DataSourceRole{}

// DataSourceRole looks up a role by name (PRO-714). It is the way to
// reference the built-in roles (System Administrator, Contributor, Reader,
// Data Sender), which the API protects from change and so cannot be a
// monad_role resource, without hard-coding their per-organization IDs.
type DataSourceRole struct {
	client *client.Client
}

type DataSourceRoleModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Protected   types.Bool   `tfsdk:"protected"`
	Permissions types.Set    `tfsdk:"permissions"`
}

func NewDataSourceRole() datasource.DataSource {
	return &DataSourceRole{}
}

func (d *DataSourceRole) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *DataSourceRole) Configure(
	ctx context.Context,
	req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *ClientData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *DataSourceRole) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an organization role by name.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Exact name of the role, such as `Reader`.",
				Required:            true,
			},
			"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Role ID."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Role description."},
			"protected": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this is a built-in role the API does not allow to be changed.",
			},
			"permissions": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Permission slugs the role grants.",
			},
		},
	}
}

func (d *DataSourceRole) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data DataSourceRoleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := data.Name.ValueString()

	var matches []monad.ModelsRoleWithPermissions
	var names []string
	const pageSize = int32(100)
	for offset := int32(0); ; offset += pageSize {
		page, httpResp, err := d.client.RolesAPI.ListRoles(ctx, d.client.OrganizationID).
			Limit(pageSize).Offset(offset).Execute()
		if err != nil {
			resp.Diagnostics.AddError("Unable to list roles", fmt.Sprintf("%s. Response: %s", err, getResponseBody(httpResp)))
			return
		}
		for _, role := range page.Roles {
			names = append(names, role.GetName())
			if role.GetName() == name {
				matches = append(matches, role)
			}
		}
		total, ok := page.Pagination.GetTotalOk()
		if len(page.Roles) == 0 || (ok && offset+int32(len(page.Roles)) >= *total) {
			break
		}
	}

	switch len(matches) {
	case 0:
		sort.Strings(names)
		resp.Diagnostics.AddError(
			"Role not found",
			fmt.Sprintf("No role named %q in this organization. Roles: %v", name, names),
		)
		return
	case 1:
	default:
		resp.Diagnostics.AddError(
			"Role name is ambiguous",
			fmt.Sprintf("%d roles are named %q; rename one so the lookup is unambiguous.", len(matches), name),
		)
		return
	}

	role := matches[0]
	data.ID = types.StringValue(role.GetId())
	data.Description = types.StringValue(role.GetDescription())
	data.Protected = types.BoolValue(role.GetProtected())
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
