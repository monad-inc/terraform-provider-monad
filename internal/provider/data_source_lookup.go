package provider

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// Lookup data sources (PRO-719): find an existing input, output, transform,
// enrichment, pipeline or secret by `id` or exact `name`, so a configuration
// can reference objects managed elsewhere (another repo, the UI) without
// pasting UUIDs. One implementation serves every kind; a lookupKind supplies
// the list and get calls and says which optional attributes the kind has.

// lookupObject is what a kind's get returns, already flattened.
type lookupObject struct {
	ID, Name, Description, Type string
	CreatedAt, UpdatedAt        string
	ManagedBy                   *monad.ModelsManagedBy
	Enabled                     *bool
	PipelineIDs                 []string
}

type lookupKind struct {
	typeName string // data source type suffix: "input" -> monad_input
	noun     string // in messages: "input"
	// which optional attributes this kind exposes
	hasType, hasManagedBy, hasEnabled, hasPipelineIDs bool
	// list reuses the resource's adopt-after-timeout lister for name lookup.
	list func(c *client.Client) adoptLister
	get  func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error)
}

var _ datasource.DataSource = &dataSourceLookup{}
var _ datasource.DataSourceWithConfigure = &dataSourceLookup{}
var _ datasource.DataSourceWithValidateConfig = &dataSourceLookup{}

type dataSourceLookup struct {
	kind   lookupKind
	client *client.Client
}

type dataSourceLookupModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Type        types.String `tfsdk:"type"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
	ManagedBy   types.String `tfsdk:"managed_by"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	PipelineIDs types.Set    `tfsdk:"pipeline_ids"`
}

func (d *dataSourceLookup) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind.typeName
}

func (d *dataSourceLookup) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dataSourceLookup) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	k := d.kind
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("ID of the %s. Set exactly one of `id` or `name`.", k.noun),
			Optional:            true,
			Computed:            true,
		},
		"name": schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("Exact, case-sensitive name of the %s. Set exactly one of `id` or `name`.", k.noun),
			Optional:            true,
			Computed:            true,
		},
		"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Description."},
		"created_at":  schema.StringAttribute{Computed: true, MarkdownDescription: "When it was created (RFC 3339)."},
		"updated_at":  schema.StringAttribute{Computed: true, MarkdownDescription: "When it was last changed (RFC 3339)."},
	}
	if k.hasType {
		attrs["type"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Connector type ID, such as `aws-s3`."}
	}
	if k.hasManagedBy {
		attrs["managed_by"] = schema.StringAttribute{Computed: true, MarkdownDescription: "`customer`, `system` or `internal`."}
	}
	if k.hasEnabled {
		attrs["enabled"] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the pipeline is enabled."}
	}
	if k.hasPipelineIDs {
		attrs["pipeline_ids"] = schema.SetAttribute{
			Computed:            true,
			ElementType:         types.StringType,
			MarkdownDescription: fmt.Sprintf("IDs of the pipelines that use this %s.", k.noun),
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf("Looks up an existing %s by ID or name.", k.noun),
		Attributes:          attrs,
	}
}

func (d *dataSourceLookup) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id, name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() || id.IsUnknown() || name.IsUnknown() {
		return
	}
	if id.IsNull() == name.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Set exactly one of id or name",
			fmt.Sprintf("Look the %s up either by `id` or by `name`, not both or neither.", d.kind.noun),
		)
	}
}

func (d *dataSourceLookup) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data dataSourceLookupModel
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("id"), &data.ID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &data.Name)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if data.ID.IsNull() {
		var err error
		id, err = d.idForName(ctx, data.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("name"), fmt.Sprintf("Unable to find %s", d.kind.noun), err.Error())
			return
		}
	}

	obj, httpResp, err := d.kind.get(ctx, d.client, id)
	if err != nil {
		body := getResponseBody(httpResp)
		if isNotFoundResponse(httpResp, body) {
			resp.Diagnostics.AddAttributeError(path.Root("id"), fmt.Sprintf("%s not found", d.kind.noun),
				fmt.Sprintf("No %s with ID %q in this organization.", d.kind.noun, id))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read %s %s: %s. Response: %s", d.kind.noun, id, err, body))
		return
	}

	data.ID = types.StringValue(obj.ID)
	data.Name = types.StringValue(obj.Name)
	data.Description = types.StringValue(obj.Description)
	data.CreatedAt = types.StringValue(obj.CreatedAt)
	data.UpdatedAt = types.StringValue(obj.UpdatedAt)

	// Attributes a kind doesn't have are absent from its schema; set them and
	// State.Set would fail, so only the ones the kind declares are written.
	attrs := map[string]any{
		"id": data.ID, "name": data.Name, "description": data.Description,
		"created_at": data.CreatedAt, "updated_at": data.UpdatedAt,
	}
	if d.kind.hasType {
		attrs["type"] = types.StringValue(obj.Type)
	}
	if d.kind.hasManagedBy {
		mb := ""
		if obj.ManagedBy != nil {
			mb = string(*obj.ManagedBy)
		}
		attrs["managed_by"] = types.StringValue(mb)
	}
	if d.kind.hasEnabled {
		attrs["enabled"] = types.BoolValue(obj.Enabled != nil && *obj.Enabled)
	}
	if d.kind.hasPipelineIDs {
		ids := append([]string(nil), obj.PipelineIDs...)
		sort.Strings(ids)
		set, diags := types.SetValueFrom(ctx, types.StringType, ids)
		resp.Diagnostics.Append(diags...)
		attrs["pipeline_ids"] = set
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(k), attrs[k])...)
	}
}

// idForName resolves an exact name to an ID through the kind's list endpoint.
func (d *dataSourceLookup) idForName(ctx context.Context, name string) (string, error) {
	list := d.kind.list(d.client)
	const pageSize = int32(100)
	var ids, names []string
	for offset := int32(0); ; offset += pageSize {
		page, total, httpResp, err := list(ctx, pageSize, offset)
		if err != nil {
			return "", fmt.Errorf("listing %ss: %s. Response: %s", d.kind.noun, err, getResponseBody(httpResp))
		}
		for _, c := range page {
			if c.Name != nil && *c.Name == name && c.ID != nil {
				ids = append(ids, *c.ID)
			}
			if c.Name != nil && len(names) < 25 {
				names = append(names, *c.Name)
			}
		}
		if len(page) == 0 || (total != nil && offset+int32(len(page)) >= *total) {
			break
		}
	}
	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		sort.Strings(names)
		return "", fmt.Errorf("no %s named %q in this organization. Some names that exist: %v", d.kind.noun, name, names)
	default:
		return "", fmt.Errorf("%d %ss are named %q; look it up by `id` instead: %v", len(ids), d.kind.noun, name, ids)
	}
}

func pipelineIDsOf(pipelines []monad.ModelsPipeline) []string {
	out := make([]string, 0, len(pipelines))
	for _, p := range pipelines {
		if p.Id != nil {
			out = append(out, *p.Id)
		}
	}
	return out
}

func newLookupDataSource(k lookupKind) func() datasource.DataSource {
	return func() datasource.DataSource { return &dataSourceLookup{kind: k} }
}

var (
	NewDataSourceInput = newLookupDataSource(lookupKind{
		typeName: "input", noun: "input", hasType: true, hasManagedBy: true, hasPipelineIDs: true,
		list: func(c *client.Client) adoptLister { return (&ResourceInput{client: c}).listForAdopt },
		get: func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error) {
			o, r, err := c.OrganizationInputsAPI.GetOrganizationInput(ctx, c.OrganizationID, id).Execute()
			if err != nil {
				return nil, r, err
			}
			return &lookupObject{ID: o.GetId(), Name: o.GetName(), Description: o.GetDescription(), Type: o.GetType(),
				CreatedAt: o.GetCreatedAt(), UpdatedAt: o.GetUpdatedAt(), ManagedBy: o.ManagedBy, PipelineIDs: pipelineIDsOf(o.ComponentOf)}, r, nil
		},
	})
	NewDataSourceOutput = newLookupDataSource(lookupKind{
		typeName: "output", noun: "output", hasType: true, hasManagedBy: true, hasPipelineIDs: true,
		list: func(c *client.Client) adoptLister { return (&ResourceOutput{client: c}).listForAdopt },
		get: func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error) {
			o, r, err := c.OrganizationOutputsAPI.GetOrganizationOutput(ctx, c.OrganizationID, id).Execute()
			if err != nil {
				return nil, r, err
			}
			return &lookupObject{ID: o.GetId(), Name: o.GetName(), Description: o.GetDescription(), Type: o.GetType(),
				CreatedAt: o.GetCreatedAt(), UpdatedAt: o.GetUpdatedAt(), ManagedBy: o.ManagedBy, PipelineIDs: pipelineIDsOf(o.ComponentOf)}, r, nil
		},
	})
	NewDataSourceTransform = newLookupDataSource(lookupKind{
		typeName: "transform", noun: "transform", hasManagedBy: true, hasPipelineIDs: true,
		list: func(c *client.Client) adoptLister { return (&ResourceTransform{client: c}).listForAdopt },
		get: func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error) {
			// The SDK's GetOrganizationTransform takes the transform ID first and
			// the organization second, unlike every other method (resource_transform.go
			// does the same).
			o, r, err := c.OrganizationTransformsAPI.GetOrganizationTransform(ctx, id, c.OrganizationID).Execute()
			if err != nil {
				return nil, r, err
			}
			return &lookupObject{ID: o.GetId(), Name: o.GetName(), Description: o.GetDescription(),
				CreatedAt: o.GetCreatedAt(), UpdatedAt: o.GetUpdatedAt(), ManagedBy: o.ManagedBy, PipelineIDs: pipelineIDsOf(o.ComponentOf)}, r, nil
		},
	})
	NewDataSourceEnrichment = newLookupDataSource(lookupKind{
		typeName: "enrichment", noun: "enrichment", hasType: true, hasManagedBy: true, hasPipelineIDs: true,
		list: func(c *client.Client) adoptLister { return (&ResourceEnrichment{client: c}).listForAdopt },
		get: func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error) {
			o, r, err := c.OrganizationEnrichmentsAPI.GetEnrichment(ctx, c.OrganizationID, id).Execute()
			if err != nil {
				return nil, r, err
			}
			return &lookupObject{ID: o.GetId(), Name: o.GetName(), Description: o.GetDescription(), Type: o.GetType(),
				CreatedAt: o.GetCreatedAt(), UpdatedAt: o.GetUpdatedAt(), ManagedBy: o.ManagedBy, PipelineIDs: pipelineIDsOf(o.ComponentOf)}, r, nil
		},
	})
	NewDataSourcePipeline = newLookupDataSource(lookupKind{
		typeName: "pipeline", noun: "pipeline", hasManagedBy: true, hasEnabled: true,
		list: func(c *client.Client) adoptLister { return (&ResourcePipeline{client: c}).listForAdopt },
		get: func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error) {
			o, r, err := c.PipelinesAPI.GetPipelineConfig(ctx, c.OrganizationID, id).Execute()
			if err != nil {
				return nil, r, err
			}
			return &lookupObject{ID: o.GetId(), Name: o.GetName(), Description: o.GetDescription(),
				CreatedAt: o.GetCreatedAt(), UpdatedAt: o.GetUpdatedAt(), ManagedBy: o.ManagedBy, Enabled: o.Enabled}, r, nil
		},
	})
	NewDataSourceSecret = newLookupDataSource(lookupKind{
		typeName: "secret", noun: "secret",
		list: func(c *client.Client) adoptLister { return (&ResourceSecret{client: c}).listForAdopt },
		get: func(ctx context.Context, c *client.Client, id string) (*lookupObject, *http.Response, error) {
			o, r, err := c.SecretsAPI.GetSecret(ctx, c.OrganizationID, id).Execute()
			if err != nil {
				return nil, r, err
			}
			// Never the value: the API does not return it, and this data source
			// exposes metadata only.
			return &lookupObject{ID: o.GetId(), Name: o.GetName(), Description: o.GetDescription(),
				CreatedAt: o.GetCreatedAt(), UpdatedAt: o.GetUpdatedAt()}, r, nil
		},
	})
)
