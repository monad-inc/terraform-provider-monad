package provider

import (
	"context"
	"fmt"
	"reflect"
	"sort"

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

// ResourcePipelineConditionConditionConfig carries every field in the API's
// edge-condition rule catalogue. Which of them a given leaf may set depends on
// its type_id — see conditionRules in condition_rules.go, which drives
// serialization, read-back and plan-time validation from one table.
type ResourcePipelineConditionConditionConfig struct {
	Key              types.String  `tfsdk:"key"`
	Value            types.String  `tfsdk:"value"`
	Values           types.List    `tfsdk:"values"`
	Pattern          types.String  `tfsdk:"pattern"`
	Percent          types.Float64 `tfsdk:"percent"`
	Rate             types.String  `tfsdk:"rate"`
	Not              types.Bool    `tfsdk:"not"`
	CaseInsensitive  types.Bool    `tfsdk:"case_insensitive"`
	Raw              types.Bool    `tfsdk:"raw"`
	Null             types.Bool    `tfsdk:"null"`
	WhitespaceString types.Bool    `tfsdk:"whitespace_string"`
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
		// v1 (ENG-9546): edge condition `config.value` changed from a list of
		// strings to a single string. See resource_pipeline_upgrade.go.
		Version: 1,
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
				// Deliberately NO order-insensitive plan modifier here — see the
				// note on "edges" below (ENG-9572).
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
				// Deliberately NO order-insensitive plan modifier.
				//
				// ENG-9221 added one that set the planned value to the prior
				// state whenever state and config held the same edge set in a
				// different order, to suppress the reorder diff after
				// `terraform import`. That is not a legal plan: Terraform
				// requires a plan-known attribute to equal the config value at
				// the SAME index, so pinning the plan to state order made every
				// position where the orders disagreed an error —
				// "Provider produced invalid plan" — which also blocked
				// destroy (ENG-9572).
				//
				// The premise is unsatisfiable for a List: when config order
				// and state order differ, no single plan can equal config
				// element-wise AND equal state, so a reorder diff after import
				// is unavoidable here. It is cosmetic and one-time — a single
				// apply normalizes it — which is strictly better than a hard
				// error. Modelling nodes/edges as Sets, which is what they
				// semantically are, is the real fix and is tracked separately.
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
												MarkdownDescription: "Configuration for the condition. Which fields apply " +
													"depends on `type_id`; setting one the rule does not read, or omitting " +
													"one it requires, is reported at plan time.",
												Attributes: map[string]schema.Attribute{
													"key": schema.StringAttribute{
														MarkdownDescription: "The key to check in the record. Use `*` to check all keys. " +
															"Required by every rule except `sample`, where it is optional and selects " +
															"hash-based sampling.",
														Optional: true,
													},
													"value": schema.StringAttribute{
														MarkdownDescription: "The single value to compare against, for `equals`, `contains`, " +
															"`starts_with`, `ends_with`, `greater_than` and `less_than`. Numeric rules " +
															"accept a numeric string (`\"100\"`). Supports JSON syntax: quoted strings, " +
															"bare words, numbers, booleans.",
														Optional: true,
													},
													"values": schema.ListAttribute{
														MarkdownDescription: "The set of values to match against, for `equals_any`.",
														Optional:            true,
														ElementType:         types.StringType,
													},
													"pattern": schema.StringAttribute{
														MarkdownDescription: "The regular expression to match against, for `matches_regex`.",
														Optional:            true,
													},
													"percent": schema.Float64Attribute{
														MarkdownDescription: "The percentage of records to pass through, for `sample`. " +
															"Examples: `12.3`, `50`.",
														Optional: true,
													},
													"rate": schema.StringAttribute{
														MarkdownDescription: "**Deprecated.** The rate at which records are passed through, " +
															"for the legacy `sample_rate` rule. Example: `'100ms'`, `'1s'`, `'1m'`. " +
															"Use `sample` with `percent` instead.",
														Optional: true,
														DeprecationMessage: "The `sample_rate` rule is superseded by `sample`. Use " +
															"type_id = \"sample\" with `percent` instead; `rate` is not surfaced in the " +
															"Monad UI and is not part of the API's published rule catalogue.",
													},
													"not": schema.BoolAttribute{
														MarkdownDescription: "Negate the result of this condition. Accepted by every rule " +
															"except `sample`.",
														Optional: true,
													},
													"case_insensitive": schema.BoolAttribute{
														MarkdownDescription: "Compare case-insensitively (strings only). Accepted by " +
															"`equals`, `equals_any`, `contains`, `starts_with` and `ends_with`.",
														Optional: true,
													},
													"raw": schema.BoolAttribute{
														MarkdownDescription: "For `contains`: treat the field value as a raw string and " +
															"substring-match it. When false, arrays and objects are checked for exact " +
															"element matches.",
														Optional: true,
													},
													"null": schema.BoolAttribute{
														MarkdownDescription: "For `is_empty`: also treat an explicit JSON null as empty.",
														Optional:            true,
													},
													"whitespace_string": schema.BoolAttribute{
														MarkdownDescription: "For `is_empty`: also treat a whitespace-only string as empty.",
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
			ComponentType: monad.ModelsComponentType(node.ComponentType.ValueString()),
			ComponentId:   node.ComponentID.ValueString(),
			Slug:          node.Slug.ValueStringPointer(),
			Enabled:       monad.PtrBool(true),
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
			Conditions: &monad.ModelsConditionEvaluatable{
				Operator: (*monad.ModelsConditionOperator)(edge.Condition.Operator.ValueStringPointer()),
			},
		}

		if len(edge.Condition.Conditions) == 0 {
			continue
		}

		out[i].Conditions.Conditions = make([]monad.ModelsConditionEvaluatable, len(edge.Condition.Conditions))
		for j, condition := range edge.Condition.Conditions {
			// Serialize per type_id: emit only the fields this rule reads, and
			// only when set. Sending the same three keys for every rule is what
			// made value comparisons silently route nothing (ENG-9546).
			out[i].Conditions.Conditions[j] = monad.ModelsConditionEvaluatable{
				TypeId: condition.TypeID.ValueStringPointer(),
				Config: buildConditionConfig(condition.TypeID.ValueString(), condition.Config),
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
		Enabled:     &enabled,
		Nodes:       buildPipelineRequestNodes(data.Nodes),
		Edges:       edges,
	}

	pipeline, monadResp, err := r.client.PipelinesAPI.CreatePipeline(
		ctx,
		r.client.OrganizationID,
	).CreatePipelineRequest(monad.RoutesV2CreatePipelineRequestAsCreatePipelineRequest(&request)).
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

	// GetPipelineConfig is the v2 read that returns the full node/edge graph.
	// GetPipeline is the v1 endpoint and returns pipeline metadata only, with
	// no nodes or edges, so it cannot drive Read.
	pipeline, monadResp, err := r.client.PipelinesAPI.
		GetPipelineConfig(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
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
			fmt.Sprintf(
				"Unable to read pipeline, got error: %s. Response: %s",
				err,
				body,
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
			ComponentType: types.StringPointerValue((*string)(node.ComponentType)),
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
			operator = types.StringPointerValue((*string)(edge.Conditions.Operator))
			conditions = make([]ResourcePipelineConditionCondition, len(edge.Conditions.Conditions))
			for j, condition := range edge.Conditions.Conditions {
				typeID := types.StringPointerValue(condition.TypeId)
				conditions[j] = ResourcePipelineConditionCondition{
					TypeID: typeID,
					Config: conditionConfigFromAPI(typeID.ValueString(), condition.Config),
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
			// Every config field must appear here. A field missing from the
			// comparison is one whose drift reconcilePipelineEdges cannot see,
			// so it would silently keep the prior state value.
			conditions[j] = map[string]any{
				"type_id":           stringOrNil(c.TypeID),
				"key":               stringOrNil(c.Config.Key),
				"value":             stringOrNil(c.Config.Value),
				"values":            listOrNil(c.Config.Values),
				"pattern":           stringOrNil(c.Config.Pattern),
				"percent":           float64OrNil(c.Config.Percent),
				"rate":              stringOrNil(c.Config.Rate),
				"not":               boolOrNil(c.Config.Not),
				"case_insensitive":  boolOrNil(c.Config.CaseInsensitive),
				"raw":               boolOrNil(c.Config.Raw),
				"null":              boolOrNil(c.Config.Null),
				"whitespace_string": boolOrNil(c.Config.WhitespaceString),
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

func boolOrNil(b types.Bool) any {
	if b.IsNull() || b.IsUnknown() {
		return nil
	}
	return b.ValueBool()
}

func float64OrNil(f types.Float64) any {
	if f.IsNull() || f.IsUnknown() {
		return nil
	}
	return f.ValueFloat64()
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
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Enabled:     &enabled,
		Nodes:       buildPipelineRequestNodes(data.Nodes),
		Edges:       edges,
	}

	pipeline, monadResp, err := r.client.PipelinesAPI.
		UpdatePipeline(
			ctx,
			r.client.OrganizationID,
			data.ID.ValueString(),
		).
		UpdatePipelineRequest(monad.RoutesV2UpdatePipelineRequestAsUpdatePipelineRequest(&request)).
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

	_, monadResp, err := r.client.PipelinesAPI.DeletePipeline(
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
