package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// Type-catalog data sources (PRO-718): the configuration schema of an input,
// output, enrichment or alert-rule type, or a transform operation, as the API
// publishes it, so configurations can discover field names, required fields,
// defaults and allowed values instead of finding mistakes as a 400 at apply.

type typeInfo struct {
	Name, Description, Vendor, Category, Granularity string
	InBeta                                           *bool
}

type typeKind struct {
	typeName string // data source type suffix
	noun     string
	idAttr   string // the lookup argument: "type_id" or "operation"
	// fieldGroups are the top-level keys of the schema to flatten into lists
	// ("settings", "secrets", "arguments").
	fieldGroups                       []string
	hasVendor, hasCategory, hasInBeta bool
	hasGranularity                    bool
	get                               func(ctx context.Context, c *client.Client, id string) (typeInfo, any, *http.Response, error)
}

var _ datasource.DataSource = &dataSourceType{}
var _ datasource.DataSourceWithConfigure = &dataSourceType{}

type dataSourceType struct {
	kind   typeKind
	client *client.Client
}

func newTypeDataSource(k typeKind) func() datasource.DataSource {
	return func() datasource.DataSource { return &dataSourceType{kind: k} }
}

func (d *dataSourceType) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind.typeName
}

func (d *dataSourceType) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func fieldListAttribute(what string) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Computed:            true,
		MarkdownDescription: fmt.Sprintf("The %s fields, flattened to dotted paths and sorted by path.", what),
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"path":           schema.StringAttribute{Computed: true, MarkdownDescription: "Dotted path, such as `bucket` or `metric_config.records.threshold`."},
				"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "Display name."},
				"type":           schema.StringAttribute{Computed: true, MarkdownDescription: "Field type, such as `string`, `int`, `bool`, `array`, `object`, `one_of`."},
				"item_type":      schema.StringAttribute{Computed: true, MarkdownDescription: "Element type of an array field."},
				"required":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the field must be set."},
				"immutable":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the field cannot change after creation."},
				"default":        schema.StringAttribute{Computed: true, MarkdownDescription: "Default value, JSON-encoded (decode with `jsondecode`); null when there is none."},
				"description":    schema.StringAttribute{Computed: true, MarkdownDescription: "What the field does."},
				"allowed_values": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Allowed values, when the field is an enum."},
			},
		},
	}
}

func (d *dataSourceType) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	k := d.kind
	attrs := map[string]schema.Attribute{
		k.idAttr: schema.StringAttribute{
			Required:            true,
			MarkdownDescription: fmt.Sprintf("The %s to describe.", k.noun),
		},
		"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Display name."},
		"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Description."},
		"schema_json": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The full schema as the API returns it, JSON-encoded, for anything the flattened lists do not express.",
		},
	}
	for _, g := range k.fieldGroups {
		attrs[g] = fieldListAttribute(g)
	}
	if k.hasVendor {
		attrs["vendor"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Vendor (the API's `house`)."}
	}
	if k.hasCategory {
		attrs["category"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Category."}
	}
	if k.hasInBeta {
		attrs["in_beta"] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the type is in beta."}
	}
	if k.hasGranularity {
		attrs["granularity"] = schema.StringAttribute{Computed: true, MarkdownDescription: "What the rule evaluates: a pipeline, the organization, or billing."}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf("Describes the configuration schema of a %s.", k.noun),
		Attributes:          attrs,
	}
}

func (d *dataSourceType) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	k := d.kind
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(k.idAttr), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	info, raw, httpResp, err := k.get(ctx, d.client, id.ValueString())
	if err != nil {
		body := getResponseBody(httpResp)
		// Most type endpoints answer an unknown ID with a 500 rather than a 404
		// (checked 2026-10-22; only outputs return 404), so treat both as
		// "probably misspelled" and say so.
		if httpResp != nil && (httpResp.StatusCode == http.StatusNotFound || httpResp.StatusCode == http.StatusInternalServerError) {
			resp.Diagnostics.AddAttributeError(path.Root(k.idAttr), fmt.Sprintf("Unknown %s", k.noun),
				fmt.Sprintf(
					"The API could not return %s %q (HTTP %d). Check the spelling: the API reports an unknown "+
						"ID as a server error for most type endpoints. Response: %s",
					k.noun, id.ValueString(), httpResp.StatusCode, body,
				))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read %s %q: %s. Response: %s", k.noun, id.ValueString(), err, body))
		return
	}

	schemaMap, _ := raw.(map[string]any)
	schemaJSON, err := json.Marshal(raw)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode schema", err.Error())
		return
	}

	set := func(name string, v any) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), v)...)
	}
	set(k.idAttr, id)
	set("name", types.StringValue(info.Name))
	set("description", types.StringValue(info.Description))
	set("schema_json", types.StringValue(string(schemaJSON)))
	for _, g := range k.fieldGroups {
		group, _ := schemaMap[g].(map[string]any)
		list, err := typeFieldsValue(flattenTypeFields(group))
		if err != nil {
			resp.Diagnostics.AddError("Unable to read schema fields", err.Error())
			return
		}
		set(g, list)
	}
	if k.hasVendor {
		set("vendor", types.StringValue(info.Vendor))
	}
	if k.hasCategory {
		set("category", types.StringValue(info.Category))
	}
	if k.hasInBeta {
		set("in_beta", types.BoolValue(info.InBeta != nil && *info.InBeta))
	}
	if k.hasGranularity {
		set("granularity", types.StringValue(info.Granularity))
	}
}

func strOf[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

var (
	NewDataSourceInputType = newTypeDataSource(typeKind{
		typeName: "input_type", noun: "input type", idAttr: "type_id",
		fieldGroups: []string{"settings", "secrets"}, hasVendor: true, hasCategory: true, hasInBeta: true,
		get: func(ctx context.Context, c *client.Client, id string) (typeInfo, any, *http.Response, error) {
			m, r, err := c.InputsAPI.GetInputTypeMeta(ctx, id).Execute()
			if err != nil {
				return typeInfo{}, nil, r, err
			}
			return typeInfo{Name: m.GetName(), Description: m.GetDescription(), Vendor: m.GetHouse(),
				Category: strOf(m.Category), InBeta: m.InBeta}, m.Config, r, nil
		},
	})
	NewDataSourceOutputType = newTypeDataSource(typeKind{
		typeName: "output_type", noun: "output type", idAttr: "type_id",
		fieldGroups: []string{"settings", "secrets"}, hasVendor: true, hasCategory: true, hasInBeta: true,
		get: func(ctx context.Context, c *client.Client, id string) (typeInfo, any, *http.Response, error) {
			m, r, err := c.OutputsAPI.GetOutputTypeMeta(ctx, id).Execute()
			if err != nil {
				return typeInfo{}, nil, r, err
			}
			return typeInfo{Name: m.GetName(), Description: m.GetDescription(), Vendor: m.GetHouse(),
				Category: strOf(m.Category), InBeta: m.InBeta}, m.Config, r, nil
		},
	})
	NewDataSourceEnrichmentType = newTypeDataSource(typeKind{
		typeName: "enrichment_type", noun: "enrichment type", idAttr: "type_id",
		fieldGroups: []string{"settings", "secrets"}, hasVendor: true, hasCategory: true, hasInBeta: true,
		get: func(ctx context.Context, c *client.Client, id string) (typeInfo, any, *http.Response, error) {
			m, r, err := c.EnrichmentsAPI.GetEnrichmentTypeMeta(ctx, c.OrganizationID, id).Execute()
			if err != nil {
				return typeInfo{}, nil, r, err
			}
			return typeInfo{Name: m.GetName(), Description: m.GetDescription(), Vendor: m.GetHouse(),
				Category: strOf(m.ConnectorCategory), InBeta: m.InBeta}, m.Config, r, nil
		},
	})
	NewDataSourceAlertRuleType = newTypeDataSource(typeKind{
		typeName: "alert_rule_type", noun: "alert rule type", idAttr: "type_id",
		fieldGroups: []string{"settings"}, hasVendor: true, hasCategory: true, hasGranularity: true,
		get: func(ctx context.Context, c *client.Client, id string) (typeInfo, any, *http.Response, error) {
			m, r, err := c.AlertRulesAPI.GetAlertRuleConfigMeta(ctx, id).Execute()
			if err != nil {
				// The API answers 200 with `"managed_by": ""`, which the SDK's
				// strict ModelsManagedBy enum refuses to decode, so every call
				// errors on a valid response. Read the body ourselves in that
				// case; when the SDK is fixed this branch simply stops running.
				if r != nil && r.StatusCode == http.StatusOK {
					return alertRuleTypeFromBody(getResponseBody(r), r)
				}
				return typeInfo{}, nil, r, err
			}
			return typeInfo{Name: m.GetName(), Description: m.GetDescription(), Vendor: strOf(m.House),
				Category: strOf(m.Category), Granularity: strOf(m.Granularity)}, m.Config, r, nil
		},
	})
	// A transform operation's schema is {operation: {...}, arguments: {...}};
	// it has no separate name, so name is the operation's display name and
	// description its description.
	NewDataSourceTransformType = newTypeDataSource(typeKind{
		typeName: "transform_type", noun: "transform operation", idAttr: "operation",
		fieldGroups: []string{"arguments"},
		get: func(ctx context.Context, c *client.Client, id string) (typeInfo, any, *http.Response, error) {
			m, r, err := c.TransformsAPI.GetTransformTypeMeta(ctx, id).Execute()
			if err != nil {
				return typeInfo{}, nil, r, err
			}
			info := typeInfo{Name: id}
			if mm, ok := m.(map[string]any); ok {
				if op, ok := mm["operation"].(map[string]any); ok {
					info.Description = str(op["description"])
				}
			}
			return info, m, r, nil
		},
	})
)

// alertRuleTypeFromBody decodes an alert rule type's metadata from the raw
// response body (see the SDK workaround in NewDataSourceAlertRuleType).
func alertRuleTypeFromBody(body []byte, r *http.Response) (typeInfo, any, *http.Response, error) {
	var m struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		House       string         `json:"house"`
		Category    string         `json:"category"`
		Granularity string         `json:"granularity"`
		Config      map[string]any `json:"config"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return typeInfo{}, nil, r, fmt.Errorf("decoding alert rule type: %w", err)
	}
	return typeInfo{Name: m.Name, Description: m.Description, Vendor: m.House,
		Category: m.Category, Granularity: m.Granularity}, m.Config, r, nil
}
