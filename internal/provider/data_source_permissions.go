package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ datasource.DataSource = &DataSourcePermissions{}
var _ datasource.DataSourceWithConfigure = &DataSourcePermissions{}

// DataSourcePermissions reads the permission catalog: every permission a role
// can grant, by slug (PRO-714).
type DataSourcePermissions struct {
	client *client.Client
}

type DataSourcePermissionsModel struct {
	ID          types.String               `tfsdk:"id"`
	Slugs       types.Set                  `tfsdk:"slugs"`
	Permissions []DataSourcePermissionItem `tfsdk:"permissions"`
}

type DataSourcePermissionItem struct {
	ID          types.String `tfsdk:"id"`
	Slug        types.String `tfsdk:"slug"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func NewDataSourcePermissions() datasource.DataSource {
	return &DataSourcePermissions{}
}

func (d *DataSourcePermissions) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_permissions"
}

func (d *DataSourcePermissions) Configure(
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

func (d *DataSourcePermissions) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The permission catalog: every permission a role can grant.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The organization ID the catalog was read for.",
				Computed:            true,
			},
			"slugs": schema.SetAttribute{
				MarkdownDescription: "Every permission slug, for `contains()` checks and `monad_role.permissions`.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"permissions": schema.ListNestedAttribute{
				MarkdownDescription: "Every permission, sorted by slug.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Permission ID."},
						"slug":        schema.StringAttribute{Computed: true, MarkdownDescription: "Slug, such as `pipeline:read`."},
						"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Display name."},
						"description": schema.StringAttribute{Computed: true, MarkdownDescription: "What the permission allows."},
					},
				},
			},
		},
	}
}

func (d *DataSourcePermissions) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	catalog, err := listAllPermissions(ctx, d.client)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the permission catalog", err.Error())
		return
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].GetSlug() < catalog[j].GetSlug() })

	data := DataSourcePermissionsModel{
		ID:          types.StringValue(d.client.OrganizationID),
		Permissions: make([]DataSourcePermissionItem, 0, len(catalog)),
	}
	for _, p := range catalog {
		data.Permissions = append(data.Permissions, DataSourcePermissionItem{
			ID:          types.StringValue(p.GetId()),
			Slug:        types.StringValue(p.GetSlug()),
			Name:        types.StringValue(p.GetName()),
			Description: types.StringValue(p.GetDescription()),
		})
	}
	slugs, diags := types.SetValueFrom(ctx, types.StringType, permissionSlugs(catalog))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Slugs = slugs

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
