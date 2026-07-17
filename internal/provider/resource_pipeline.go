package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
			},
		},
		Blocks: map[string]schema.Block{
			"nodes": schema.ListNestedBlock{
				MarkdownDescription: "List of nodes in the pipeline",
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
	// nodes/edges/enabled are preserved from prior state: they are user-authored
	// and rebuilding them from the API response reintroduces representation
	// differences (see Create) that would show as a perpetual diff.

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
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
