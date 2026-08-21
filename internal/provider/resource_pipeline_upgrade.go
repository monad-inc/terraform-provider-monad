package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithUpgradeState = &ResourcePipeline{}

// Schema version 1 (ENG-9546) changed the edge-condition `config.value`
// attribute from a list of strings to a single string, so prior state cannot be
// decoded against the current schema without an upgrade.
//
// The upgrade moves the old list into the new `values` attribute, which is the
// structurally faithful move and the correct final shape for `equals_any`. It
// deliberately does not guess a scalar from a one-element list: the practitioner
// edits their HCL regardless (a list literal is no longer valid for `value`),
// and inventing a value here would hide that edit behind a clean plan.
func (r *ResourcePipeline) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema:   pipelineSchemaV0(),
			StateUpgrader: upgradePipelineStateV0toV1,
		},
	}
}

// resourcePipelineModelV0 mirrors ResourcePipelineModel as it stood at schema
// version 0. Only the condition config differs; the rest is repeated because
// the framework decodes the whole resource against the prior schema.
type resourcePipelineModelV0 struct {
	ID          types.String             `tfsdk:"id"`
	Name        types.String             `tfsdk:"name"`
	Description types.String             `tfsdk:"description"`
	Nodes       []ResourcePipelineNode   `tfsdk:"nodes"`
	Edges       []resourcePipelineEdgeV0 `tfsdk:"edges"`
	Enabled     types.Bool               `tfsdk:"enabled"`
}

type resourcePipelineEdgeV0 struct {
	Name                 types.String                `tfsdk:"name"`
	Description          types.String                `tfsdk:"description"`
	FromNodeInstanceSlug types.String                `tfsdk:"from_node_instance_slug"`
	ToNodeInstanceSlug   types.String                `tfsdk:"to_node_instance_slug"`
	Condition            resourcePipelineConditionV0 `tfsdk:"condition"`
}

type resourcePipelineConditionV0 struct {
	Operator   types.String                           `tfsdk:"operator"`
	Conditions []resourcePipelineConditionConditionV0 `tfsdk:"conditions"`
}

type resourcePipelineConditionConditionV0 struct {
	TypeID types.String                       `tfsdk:"type_id"`
	Config resourcePipelineConditionConfigV0R `tfsdk:"config"`
}

type resourcePipelineConditionConfigV0R struct {
	Key   types.String `tfsdk:"key"`
	Value types.List   `tfsdk:"value"`
	Rate  types.String `tfsdk:"rate"`
}

func upgradePipelineStateV0toV1(
	ctx context.Context,
	req resource.UpgradeStateRequest,
	resp *resource.UpgradeStateResponse,
) {
	var prior resourcePipelineModelV0
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	upgraded := ResourcePipelineModel{
		ID:          prior.ID,
		Name:        prior.Name,
		Description: prior.Description,
		Nodes:       prior.Nodes,
		Enabled:     prior.Enabled,
		Edges:       make([]ResourcePipelineEdge, len(prior.Edges)),
	}

	for i, edge := range prior.Edges {
		conditions := make([]ResourcePipelineConditionCondition, len(edge.Condition.Conditions))
		for j, leaf := range edge.Condition.Conditions {
			values := types.ListNull(types.StringType)
			if !leaf.Config.Value.IsNull() && !leaf.Config.Value.IsUnknown() &&
				len(leaf.Config.Value.Elements()) > 0 {
				values = leaf.Config.Value
			}

			conditions[j] = ResourcePipelineConditionCondition{
				TypeID: leaf.TypeID,
				Config: ResourcePipelineConditionConditionConfig{
					Key:              leaf.Config.Key,
					Value:            types.StringNull(),
					Values:           values,
					Pattern:          types.StringNull(),
					Percent:          types.Float64Null(),
					Rate:             leaf.Config.Rate,
					Not:              types.BoolNull(),
					CaseInsensitive:  types.BoolNull(),
					Raw:              types.BoolNull(),
					Null:             types.BoolNull(),
					WhitespaceString: types.BoolNull(),
				},
			}
		}

		upgraded.Edges[i] = ResourcePipelineEdge{
			Name:                 edge.Name,
			Description:          edge.Description,
			FromNodeInstanceSlug: edge.FromNodeInstanceSlug,
			ToNodeInstanceSlug:   edge.ToNodeInstanceSlug,
			Condition: ResourcePipelineCondition{
				Operator:   edge.Condition.Operator,
				Conditions: conditions,
			},
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, upgraded)...)
}

// pipelineSchemaV0 is the resource schema as it stood before ENG-9546. It exists
// only so the framework can decode prior state; keep it frozen.
func pipelineSchemaV0() *schema.Schema {
	return &schema.Schema{
		MarkdownDescription: "Monad Pipeline",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"enabled":     schema.BoolAttribute{Optional: true, Computed: true},
		},
		Blocks: map[string]schema.Block{
			"nodes": schema.ListNestedBlock{
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"component_type": schema.StringAttribute{Required: true},
						"component_id":   schema.StringAttribute{Required: true},
						"slug":           schema.StringAttribute{Optional: true},
					},
				},
			},
			"edges": schema.ListNestedBlock{
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name":                    schema.StringAttribute{Optional: true},
						"description":             schema.StringAttribute{Optional: true},
						"from_node_instance_slug": schema.StringAttribute{Required: true},
						"to_node_instance_slug":   schema.StringAttribute{Required: true},
					},
					Blocks: map[string]schema.Block{
						"condition": schema.SingleNestedBlock{
							Attributes: map[string]schema.Attribute{
								"operator": schema.StringAttribute{Required: true},
							},
							Blocks: map[string]schema.Block{
								"conditions": schema.ListNestedBlock{
									NestedObject: schema.NestedBlockObject{
										Attributes: map[string]schema.Attribute{
											"type_id": schema.StringAttribute{Optional: true},
										},
										Blocks: map[string]schema.Block{
											"config": schema.SingleNestedBlock{
												Attributes: map[string]schema.Attribute{
													"key": schema.StringAttribute{Optional: true},
													"value": schema.ListAttribute{
														Optional:    true,
														ElementType: types.StringType,
													},
													"rate": schema.StringAttribute{Optional: true},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
