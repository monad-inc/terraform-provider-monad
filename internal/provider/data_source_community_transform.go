package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ datasource.DataSource = &DataSourceCommunityTransform{}
var _ datasource.DataSourceWithConfigure = &DataSourceCommunityTransform{}

// DataSourceCommunityTransform reads a published transform from Monad's
// community transform repository (PRO-718), so its `config` can be passed
// straight into a monad_transform.
type DataSourceCommunityTransform struct {
	client *client.Client
}

type DataSourceCommunityTransformModel struct {
	ID           types.String  `tfsdk:"id"`
	Name         types.String  `tfsdk:"name"`
	Description  types.String  `tfsdk:"description"`
	Author       types.String  `tfsdk:"author"`
	Contributors types.List    `tfsdk:"contributors"`
	Tags         types.List    `tfsdk:"tags"`
	Inputs       types.List    `tfsdk:"inputs"`
	Config       types.Dynamic `tfsdk:"config"`
	ConfigJSON   types.String  `tfsdk:"config_json"`
}

func NewDataSourceCommunityTransform() datasource.DataSource {
	return &DataSourceCommunityTransform{}
}

func (d *DataSourceCommunityTransform) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_community_transform"
}

func (d *DataSourceCommunityTransform) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *DataSourceCommunityTransform) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	strList := func(desc string) schema.ListAttribute {
		return schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a transform from Monad's community transform repository.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Repository path of the transform, such as " +
					"`ocsf:v1.9.0:okta:okta-systemlog`, as listed in the Monad UI or the API.",
			},
			"name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Name."},
			"description":  schema.StringAttribute{Computed: true, MarkdownDescription: "Description."},
			"author":       schema.StringAttribute{Computed: true, MarkdownDescription: "Author."},
			"contributors": strList("Contributors."),
			"tags":         strList("Tags."),
			"inputs":       strList("Input type IDs the transform is written for."),
			"config": schema.DynamicAttribute{
				Computed:            true,
				MarkdownDescription: "The transform's configuration, ready for `monad_transform.config`.",
			},
			"config_json": schema.StringAttribute{Computed: true, MarkdownDescription: "The same configuration, JSON-encoded."},
		},
	}
}

func (d *DataSourceCommunityTransform) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data DataSourceCommunityTransformModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	t, httpResp, err := d.client.TransformsRepositoryAPI.GetTransformDetailsFromRepository(ctx, data.ID.ValueString()).Execute()
	if err != nil {
		body := getResponseBody(httpResp)
		if isNotFoundResponse(httpResp, body) {
			resp.Diagnostics.AddError("Community transform not found",
				fmt.Sprintf("The repository has no transform %q.", data.ID.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to read community transform %q: %s. Response: %s", data.ID.ValueString(), err, body))
		return
	}

	list := func(in []string) types.List {
		out := append([]string(nil), in...)
		sort.Strings(out)
		v, diags := types.ListValueFrom(ctx, types.StringType, out)
		resp.Diagnostics.Append(diags...)
		return v
	}
	data.Name = types.StringValue(t.GetName())
	data.Description = types.StringValue(t.GetDescription())
	data.Author = types.StringValue(t.GetAuthor())
	data.Contributors = list(t.Contributors)
	data.Tags = list(t.Tags)
	data.Inputs = list(t.Inputs)

	cfg, err := AnyToDynamic(t.Config)
	if err != nil {
		resp.Diagnostics.AddError("Unable to convert transform config", err.Error())
		return
	}
	data.Config = cfg
	b, err := json.Marshal(t.Config)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode transform config", err.Error())
		return
	}
	data.ConfigJSON = types.StringValue(string(b))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
