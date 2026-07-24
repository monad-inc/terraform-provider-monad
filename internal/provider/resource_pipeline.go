package provider

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourcePipeline{}
var _ resource.ResourceWithConfigure = &ResourcePipeline{}
var _ resource.ResourceWithImportState = &ResourcePipeline{}

type ResourcePipeline struct {
	client *client.Client
}

type ResourcePipelineModel struct {
	ID          types.String           `tfsdk:"id"`
	Name        types.String           `tfsdk:"name"`
	Description types.String           `tfsdk:"description"`
	Nodes       []ResourcePipelineNode `tfsdk:"nodes"`
	Edges       []ResourcePipelineEdge `tfsdk:"edges"`
	Enabled     types.Bool             `tfsdk:"enabled"`
}

type ResourcePipelineNode struct {
	ComponentType types.String `tfsdk:"component_type"`
	ComponentID   types.String `tfsdk:"component_id"`
	Slug          types.String `tfsdk:"slug"`
}

type ResourcePipelineEdge struct {
	Name                 types.String              `tfsdk:"name"`
	Description          types.String              `tfsdk:"description"`
	FromNodeInstanceSlug types.String              `tfsdk:"from_node_instance_slug"`
	ToNodeInstanceSlug   types.String              `tfsdk:"to_node_instance_slug"`
	Condition            ResourcePipelineCondition `tfsdk:"condition"`
}

type ResourcePipelineCondition struct {
	Operator   types.String                         `tfsdk:"operator"`
	Conditions []ResourcePipelineConditionCondition `tfsdk:"conditions"`
}

type ResourcePipelineConditionCondition struct {
	TypeID types.String                             `tfsdk:"type_id"`
	Config ResourcePipelineConditionConditionConfig `tfsdk:"config"`
}

type ResourcePipelineConditionConditionConfig struct {
	Key   types.String `tfsdk:"key"`
	Value types.List   `tfsdk:"value"`
	Rate  types.String `tfsdk:"rate"`
}

func NewResourcePipeline() resource.Resource {
	return &ResourcePipeline{}
}

func (r *ResourcePipeline) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (r *ResourcePipeline) Configure(
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

func (r *ResourcePipeline) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Pipeline",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Pipeline identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the pipeline",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the pipeline",
				Optional:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the pipeline is enabled",
				Optional:            true,
				// Computed so the server's value populates on import (and when
				// the practitioner omits it), giving a clean first plan instead
				// of a spurious `enabled` change (ENG-9221). UseStateForUnknown
				// keeps an omitted value stable across plans rather than
				// re-reading it as unknown.
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"nodes": schema.ListNestedBlock{
				MarkdownDescription: "List of nodes in the pipeline",
				// Node order is not semantically meaningful; suppress a plan
				// diff that only reorders an unchanged node set — most notably
				// the first plan after `terraform import` (ENG-9221).
				PlanModifiers: []planmodifier.List{
					pipelineNodesOrderInsensitive{},
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"component_type": schema.StringAttribute{
							MarkdownDescription: "Type of the component",
							Required:            true,
						},
						"component_id": schema.StringAttribute{
							MarkdownDescription: "ID of the component",
							Required:            true,
						},
						"slug": schema.StringAttribute{
							MarkdownDescription: "Slug for the node",
							Optional:            true,
						},
					},
				},
			},
			"edges": schema.ListNestedBlock{
				MarkdownDescription: "List of edges in the pipeline",
				// Edge order is not semantically meaningful; suppress a plan
				// diff that only reorders an unchanged edge set — most notably
				// the first plan after `terraform import` (ENG-9221).
				PlanModifiers: []planmodifier.List{
					pipelineEdgesOrderInsensitive{},
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the edge",
							Optional:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Description of the edge",
							Optional:            true,
						},
						"from_node_instance_slug": schema.StringAttribute{
							MarkdownDescription: "Slug of the source node instance",
							Required:            true,
						},
						"to_node_instance_slug": schema.StringAttribute{
							MarkdownDescription: "Slug of the target node instance",
							Required:            true,
						},
					},
					Blocks: map[string]schema.Block{
						"condition": schema.SingleNestedBlock{
							MarkdownDescription: "Conditions for the edge",
							Attributes: map[string]schema.Attribute{
								"operator": schema.StringAttribute{
									MarkdownDescription: "Operator for the condition",
									Required:            true,
								},
							},
							Blocks: map[string]schema.Block{
								"conditions": schema.ListNestedBlock{
									MarkdownDescription: "Nested conditions for the edge",
									NestedObject: schema.NestedBlockObject{
										Attributes: map[string]schema.Attribute{
											"type_id": schema.StringAttribute{
												MarkdownDescription: "Type ID for the condition",
												Optional:            true,
											},
										},
										Blocks: map[string]schema.Block{
											"config": schema.SingleNestedBlock{
												MarkdownDescription: "Configuration for the condition",
												Attributes: map[string]schema.Attribute{
													"key": schema.StringAttribute{
														MarkdownDescription: "The key to check for in the record",
														Optional:            true,
													},
													"value": schema.ListAttribute{
														MarkdownDescription: "The string values to check for in the record",
														Optional:            true,
														ElementType:         types.StringType,
													},
													"rate": schema.StringAttribute{
														MarkdownDescription: "The rate at which records should be passed through the condition. Example: '100ms', '1s', '1m'",
														Optional:            true,
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
		},
	}
}

// buildPipelineRequestNodes/Edges translate the plan model into the SDK request
// shape shared by Create and Update.
func buildPipelineRequestNodes(nodes []ResourcePipelineNode) []monad.RoutesV2PipelineRequestNode {
	out := make([]monad.RoutesV2PipelineRequestNode, len(nodes))
	for i, node := range nodes {
		out[i] = monad.RoutesV2PipelineRequestNode{
			ComponentType: node.ComponentType.ValueString(),
			ComponentId:   node.ComponentID.ValueString(),
			Slug:          node.Slug.ValueStringPointer(),
			Enabled:       true,
		}
	}
	return out
}

func buildPipelineRequestEdges(ctx context.Context, edges []ResourcePipelineEdge) ([]monad.RoutesV2PipelineRequestEdge, error) {
	out := make([]monad.RoutesV2PipelineRequestEdge, len(edges))
	for i, edge := range edges {
		out[i] = monad.RoutesV2PipelineRequestEdge{
			Name:               edge.Name.ValueStringPointer(),
			Description:        edge.Description.ValueStringPointer(),
			FromNodeInstanceId: edge.FromNodeInstanceSlug.ValueString(),
			ToNodeInstanceId:   edge.ToNodeInstanceSlug.ValueString(),
			Conditions: &monad.ModelsPipelineEdgeConditions{
				Operator: edge.Condition.Operator.ValueStringPointer(),
			},
		}

		if len(edge.Condition.Conditions) == 0 {
			continue
		}

		out[i].Conditions.Conditions = make([]monad.ModelsPipelineEdgeCondition, len(edge.Condition.Conditions))
		for j, condition := range edge.Condition.Conditions {
			values := make([]string, 0)
			if !condition.Config.Value.IsNull() {
				if diag := condition.Config.Value.ElementsAs(ctx, &values, false); diag.HasError() {
					return nil, fmt.Errorf("failed to read condition values for edge %d condition %d", i, j)
				}
			}

			out[i].Conditions.Conditions[j] = monad.ModelsPipelineEdgeCondition{
				TypeId: condition.TypeID.ValueStringPointer(),
				Config: map[string]any{
					"key":   condition.Config.Key.ValueString(),
					"value": values,
					"rate":  condition.Config.Rate.ValueString(),
				},
			}
		}
	}
	return out, nil
}

func (r *ResourcePipeline) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourcePipelineModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	enabled := true
	if !data.Enabled.IsNull() {
		enabled = data.Enabled.ValueBool()
	}

	edges, err := buildPipelineRequestEdges(ctx, data.Edges)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build pipeline edges", err.Error())
		return
	}

	request := monad.RoutesV2CreatePipelineRequest{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueStringPointer(),
		Enabled:     enabled,
		Nodes:       buildPipelineRequestNodes(data.Nodes),
		Edges:       edges,
	}

	pipeline, monadResp, err := r.client.PipelinesAPI.V2OrganizationIdPipelinesPost(
		ctx,
		r.client.OrganizationID,
	).RoutesV2CreatePipelineRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create pipeline, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Only the computed `id` comes from the response. name/description/enabled
	// and the nodes/edges blocks are plan-known and already in `data`.
	// Rebuilding them from the API response reintroduces server-side
	// representation differences — nullable edge name/description, omitted node
	// slugs, node ordering, server-generated node-instance ids — that trip
	// "Provider produced inconsistent result after apply" and cause perpetual
	// diffs.
	data.ID = types.StringValue(*pipeline.Id)
	// `enabled` is Optional+Computed; resolve any unknown (omitted config) to
	// the value actually sent so state is known and consistent.
	data.Enabled = types.BoolValue(enabled)

	tflog.Trace(ctx, "created a pipeline resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourcePipeline) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourcePipelineModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pipeline, monadResp, err := r.client.PipelinesAPI.
		V2OrganizationIdPipelinesPipelineIdGet(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read pipeline, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	description := types.StringNull()
	if pipeline.Description != nil && *pipeline.Description != "" {
		description = types.StringValue(*pipeline.Description)
	}

	data.ID = types.StringValue(*pipeline.Id)
	data.Name = types.StringValue(*pipeline.Name)
	data.Description = description

	// Refresh `enabled` so a pipeline toggled outside Terraform (e.g. in the UI)
	// surfaces as drift in the next plan.
	// `enabled` is Computed and reflects the true server value; setting it
	// directly surfaces a UI-side toggle as drift and gives a clean plan on
	// import (ENG-9221). An omitted-config value stays stable via
	// UseStateForUnknown, so this no longer churns.
	data.Enabled = types.BoolValue(pipeline.GetEnabled())

	// Reconcile nodes/edges for drift without reintroducing the perpetual diffs
	// that motivated preserving them: the API assigns node-instance ids, may
	// generate slugs the practitioner omitted, echoes nullable edge
	// name/description, and returns nodes/edges in server order. We rebuild the
	// API view (mapping node-instance ids back to config slugs, sorted to the
	// prior order), then keep the prior state verbatim when it is semantically
	// equal — masking server-populated fields the practitioner left null so
	// they never read as drift. On import prior state is empty, so the API view
	// populates. Only genuine topology drift is written back.
	data.Nodes = reconcilePipelineNodes(data.Nodes, buildPipelineStateNodes(pipeline, data.Nodes))
	data.Edges = reconcilePipelineEdges(data.Edges, buildPipelineStateEdges(pipeline, data.Edges))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// buildPipelineStateNodes reconstructs the node list from an API response,
// mapped into the Terraform model and sorted to match the prior config order.
func buildPipelineStateNodes(pipeline *monad.ModelsPipelineConfigV2, priorNodes []ResourcePipelineNode) []ResourcePipelineNode {
	nodes := make([]ResourcePipelineNode, len(pipeline.Nodes))
	for i, node := range pipeline.Nodes {
		slug := types.StringNull()
		if node.Slug != nil {
			slug = types.StringValue(*node.Slug)
		}
		nodes[i] = ResourcePipelineNode{
			ComponentType: types.StringPointerValue(node.ComponentType),
			ComponentID:   types.StringPointerValue(node.ComponentId),
			Slug:          slug,
		}
	}
	sortNodesByConfigOrder(nodes, priorNodes)
	return nodes
}

// buildPipelineStateEdges reconstructs the edge list from an API response,
// resolving node-instance ids back to config slugs and sorting to prior order.
func buildPipelineStateEdges(pipeline *monad.ModelsPipelineConfigV2, priorEdges []ResourcePipelineEdge) []ResourcePipelineEdge {
	edges := make([]ResourcePipelineEdge, len(pipeline.Edges))
	for i, edge := range pipeline.Edges {
		name := types.StringNull()
		if edge.Name != nil {
			name = types.StringValue(*edge.Name)
		}

		description := types.StringNull()
		if edge.Description != nil {
			description = types.StringValue(*edge.Description)
		}

		operator := types.StringNull()
		conditions := []ResourcePipelineConditionCondition{}
		if edge.Conditions != nil {
			operator = types.StringPointerValue(edge.Conditions.Operator)
			conditions = make([]ResourcePipelineConditionCondition, len(edge.Conditions.Conditions))
			for j, condition := range edge.Conditions.Conditions {
				key := types.StringNull()
				if k, ok := condition.Config["key"].(string); ok && k != "" {
					key = types.StringValue(k)
				}

				rate := types.StringNull()
				if rt, ok := condition.Config["rate"].(string); ok && rt != "" {
					rate = types.StringValue(rt)
				}

				value := types.ListNull(types.StringType)
				if v, ok := condition.Config["value"].([]interface{}); ok && len(v) > 0 {
					values := make([]attr.Value, len(v))
					for k, val := range v {
						if strVal, ok := val.(string); ok {
							values[k] = types.StringValue(strVal)
						}
					}
					value = types.ListValueMust(types.StringType, values)
				}

				conditions[j] = ResourcePipelineConditionCondition{
					TypeID: types.StringPointerValue(condition.TypeId),
					Config: ResourcePipelineConditionConditionConfig{
						Key:   key,
						Value: value,
						Rate:  rate,
					},
				}
			}
		}

		fromSlug := ""
		if edge.FromNodeInstanceId != nil {
			fromSlug = getSlugForNodeID(pipeline.Nodes, *edge.FromNodeInstanceId)
		}
		toSlug := ""
		if edge.ToNodeInstanceId != nil {
			toSlug = getSlugForNodeID(pipeline.Nodes, *edge.ToNodeInstanceId)
		}

		edges[i] = ResourcePipelineEdge{
			Name:                 name,
			Description:          description,
			FromNodeInstanceSlug: types.StringValue(fromSlug),
			ToNodeInstanceSlug:   types.StringValue(toSlug),
			Condition: ResourcePipelineCondition{
				Operator:   operator,
				Conditions: conditions,
			},
		}
	}
	sortEdgesByConfigOrder(edges, priorEdges)
	return edges
}

func getSlugForNodeID(nodes []monad.ModelsPipelineNode, nodeID string) string {
	for _, node := range nodes {
		if node.Id != nil && *node.Id == nodeID && node.Slug != nil {
			return *node.Slug
		}
	}
	return ""
}

func sortNodesByConfigOrder(nodes []ResourcePipelineNode, configNodes []ResourcePipelineNode) {
	configOrder := make(map[string]int)
	for i, node := range configNodes {
		configOrder[node.ComponentID.ValueString()] = i
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		orderI, okI := configOrder[nodes[i].ComponentID.ValueString()]
		orderJ, okJ := configOrder[nodes[j].ComponentID.ValueString()]

		if okI && okJ {
			return orderI < orderJ
		}
		if okI {
			return true
		}
		if okJ {
			return false
		}
		return nodes[i].ComponentID.ValueString() < nodes[j].ComponentID.ValueString()
	})
}

func sortEdgesByConfigOrder(edges []ResourcePipelineEdge, configEdges []ResourcePipelineEdge) {
	edgeKey := func(e ResourcePipelineEdge) string {
		return e.FromNodeInstanceSlug.ValueString() + "->" + e.ToNodeInstanceSlug.ValueString()
	}

	configOrder := make(map[string]int)
	for i, edge := range configEdges {
		configOrder[edgeKey(edge)] = i
	}

	sort.SliceStable(edges, func(i, j int) bool {
		keyI := edgeKey(edges[i])
		keyJ := edgeKey(edges[j])
		orderI, okI := configOrder[keyI]
		orderJ, okJ := configOrder[keyJ]

		if okI && okJ {
			return orderI < orderJ
		}
		if okI {
			return true
		}
		if okJ {
			return false
		}
		return keyI < keyJ
	})
}

// reconcilePipelineNodes keeps the prior state node list when it is
// semantically equal to the API-derived list, so genuine drift surfaces while
// the practitioner-authored representation (including omitted, server-generated
// slugs) is preserved. Slugs the practitioner left null are masked out of the
// comparison so the server-assigned value never reads as drift.
func reconcilePipelineNodes(prior, api []ResourcePipelineNode) []ResourcePipelineNode {
	if len(prior) == 0 {
		return api
	}

	priorSlugNull := make(map[string]bool, len(prior))
	for _, n := range prior {
		priorSlugNull[n.ComponentID.ValueString()] = n.Slug.IsNull()
	}

	masked := make([]ResourcePipelineNode, len(api))
	for i, n := range api {
		if priorSlugNull[n.ComponentID.ValueString()] {
			n.Slug = types.StringNull()
		}
		masked[i] = n
	}

	if reflect.DeepEqual(jsonNormalize(pipelineNodesComparable(prior)), jsonNormalize(pipelineNodesComparable(masked))) {
		return prior
	}
	return api
}

// reconcilePipelineEdges mirrors reconcilePipelineNodes for edges. Nullable
// edge name/description that the practitioner omitted are masked so the
// server-echoed values do not read as drift. Edges are matched positionally,
// both lists having been sorted to the prior config order.
func reconcilePipelineEdges(prior, api []ResourcePipelineEdge) []ResourcePipelineEdge {
	if len(prior) == 0 {
		return api
	}

	masked := make([]ResourcePipelineEdge, len(api))
	copy(masked, api)
	for i := range masked {
		if i >= len(prior) {
			break
		}
		if prior[i].Name.IsNull() {
			masked[i].Name = types.StringNull()
		}
		if prior[i].Description.IsNull() {
			masked[i].Description = types.StringNull()
		}
	}

	if reflect.DeepEqual(jsonNormalize(pipelineEdgesComparable(prior)), jsonNormalize(pipelineEdgesComparable(masked))) {
		return prior
	}
	return api
}

func pipelineNodesComparable(nodes []ResourcePipelineNode) []any {
	out := make([]any, len(nodes))
	for i, n := range nodes {
		out[i] = map[string]any{
			"component_type": stringOrNil(n.ComponentType),
			"component_id":   stringOrNil(n.ComponentID),
			"slug":           stringOrNil(n.Slug),
		}
	}
	return out
}

func pipelineEdgesComparable(edges []ResourcePipelineEdge) []any {
	out := make([]any, len(edges))
	for i, e := range edges {
		conditions := make([]any, len(e.Condition.Conditions))
		for j, c := range e.Condition.Conditions {
			conditions[j] = map[string]any{
				"type_id": stringOrNil(c.TypeID),
				"key":     stringOrNil(c.Config.Key),
				"rate":    stringOrNil(c.Config.Rate),
				"value":   listOrNil(c.Config.Value),
			}
		}
		out[i] = map[string]any{
			"name":        stringOrNil(e.Name),
			"description": stringOrNil(e.Description),
			"from":        stringOrNil(e.FromNodeInstanceSlug),
			"to":          stringOrNil(e.ToNodeInstanceSlug),
			"operator":    stringOrNil(e.Condition.Operator),
			"conditions":  conditions,
		}
	}
	return out
}

func stringOrNil(s types.String) any {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	return s.ValueString()
}

func listOrNil(l types.List) any {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]any, 0, len(l.Elements()))
	for _, e := range l.Elements() {
		if s, ok := e.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	return out
}

func (r *ResourcePipeline) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourcePipelineModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	enabled := true
	if !data.Enabled.IsNull() {
		enabled = data.Enabled.ValueBool()
	}

	edges, err := buildPipelineRequestEdges(ctx, data.Edges)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build pipeline edges", err.Error())
		return
	}

	request := monad.RoutesV2UpdatePipelineRequest{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueStringPointer(),
		Enabled:     enabled,
		Nodes:       buildPipelineRequestNodes(data.Nodes),
		Edges:       edges,
	}

	pipeline, monadResp, err := r.client.PipelinesAPI.
		V2OrganizationIdPipelinesPipelineIdPatch(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		RoutesV2UpdatePipelineRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update pipeline, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Preserve plan-known values (see Create); only the computed `id` is taken
	// from the response.
	data.ID = types.StringValue(*pipeline.Id)
	data.Enabled = types.BoolValue(enabled)

	tflog.Trace(ctx, "updated a pipeline resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourcePipeline) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourcePipelineModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, monadResp, err := r.client.PipelinesAPI.V2OrganizationIdPipelinesPipelineIdDelete(
		ctx,
		r.client.OrganizationID,
		data.ID.ValueString(),
	).Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete pipeline, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}
}

func (r *ResourcePipeline) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
