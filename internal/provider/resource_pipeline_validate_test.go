package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Graph validation mirrors core/pkg/pipeline_validation/rules.go so an invalid
// topology is caught at plan rather than as a 400 part-way through an apply.
// The multiple-incoming-edges case is the one that actually bit: the ENG-9546
// verification probe fanned two branches into one sink and failed only after
// four components had already been created in the org.

func graphNode(slug, ctype string) ResourcePipelineNode {
	return ResourcePipelineNode{
		ComponentType: types.StringValue(ctype),
		ComponentID:   types.StringValue("id-" + slug),
		Slug:          types.StringValue(slug),
	}
}

func graphEdge(from, to string) ResourcePipelineEdge {
	return ResourcePipelineEdge{
		Name:                 types.StringNull(),
		Description:          types.StringNull(),
		FromNodeInstanceSlug: types.StringValue(from),
		ToNodeInstanceSlug:   types.StringValue(to),
		Condition: ResourcePipelineCondition{
			Operator: types.StringValue("always"),
		},
	}
}

func validateGraph(nodes []ResourcePipelineNode, edges []ResourcePipelineEdge) *resource.ValidateConfigResponse {
	resp := &resource.ValidateConfigResponse{}
	validatePipelineGraph(ResourcePipelineModel{Nodes: nodes, Edges: edges}, resp)
	return resp
}

func summaries(resp *resource.ValidateConfigResponse) []string {
	out := make([]string, 0, resp.Diagnostics.ErrorsCount())
	for _, d := range resp.Diagnostics.Errors() {
		out = append(out, d.Summary())
	}
	return out
}

// The shape ENG-9546's own probe uses: one input fanning into two arms, each
// with its own sink. This must stay valid — a fan-out is legal, only a fan-IN is
// not.
func TestValidatePipelineGraphAcceptsFanOut(t *testing.T) {
	resp := validateGraph(
		[]ResourcePipelineNode{
			graphNode("in", "input"),
			graphNode("arm-a", "transform"),
			graphNode("arm-b", "transform"),
			graphNode("sink-a", "output"),
			graphNode("sink-b", "output"),
		},
		[]ResourcePipelineEdge{
			graphEdge("in", "arm-a"),
			graphEdge("in", "arm-b"),
			graphEdge("arm-a", "sink-a"),
			graphEdge("arm-b", "sink-b"),
		},
	)
	assert.False(t, resp.Diagnostics.HasError(), "fan-out must validate: %v", summaries(resp))
}

func TestValidatePipelineGraphRejectsFanIn(t *testing.T) {
	resp := validateGraph(
		[]ResourcePipelineNode{
			graphNode("in", "input"),
			graphNode("arm-a", "transform"),
			graphNode("arm-b", "transform"),
			graphNode("sink", "output"),
		},
		[]ResourcePipelineEdge{
			graphEdge("in", "arm-a"),
			graphEdge("in", "arm-b"),
			graphEdge("arm-a", "sink"),
			graphEdge("arm-b", "sink"),
		},
	)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, summaries(resp), "Node has multiple incoming edges")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "arm-a")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "arm-b")
}

func TestValidatePipelineGraph(t *testing.T) {
	cases := []struct {
		name    string
		nodes   []ResourcePipelineNode
		edges   []ResourcePipelineEdge
		wantSum string
	}{
		{
			name: "edge names an unknown slug",
			nodes: []ResourcePipelineNode{
				graphNode("in", "input"),
				graphNode("sink", "output"),
			},
			edges:   []ResourcePipelineEdge{graphEdge("in", "sinkk")},
			wantSum: "Unknown node slug",
		},
		{
			name: "duplicate node slug",
			nodes: []ResourcePipelineNode{
				graphNode("in", "input"),
				graphNode("in", "transform"),
				graphNode("sink", "output"),
			},
			edges:   []ResourcePipelineEdge{graphEdge("in", "sink")},
			wantSum: "Duplicate node slug",
		},
		{
			name: "two roots",
			nodes: []ResourcePipelineNode{
				graphNode("in-a", "input"),
				graphNode("in-b", "input"),
				graphNode("sink", "output"),
			},
			edges:   []ResourcePipelineEdge{graphEdge("in-a", "sink")},
			wantSum: "Pipeline must have exactly one root node",
		},
		{
			name: "root is not an input",
			nodes: []ResourcePipelineNode{
				graphNode("start", "transform"),
				graphNode("sink", "output"),
			},
			edges:   []ResourcePipelineEdge{graphEdge("start", "sink")},
			wantSum: "Root node must be an input",
		},
		{
			name: "output has an outgoing edge",
			nodes: []ResourcePipelineNode{
				graphNode("in", "input"),
				graphNode("sink", "output"),
				graphNode("after", "output"),
			},
			edges: []ResourcePipelineEdge{
				graphEdge("in", "sink"),
				graphEdge("sink", "after"),
			},
			wantSum: "Output nodes cannot have outgoing edges",
		},
		{
			name: "middle node is an output",
			nodes: []ResourcePipelineNode{
				graphNode("in", "input"),
				graphNode("mid", "input"),
				graphNode("sink", "output"),
			},
			edges: []ResourcePipelineEdge{
				graphEdge("in", "mid"),
				graphEdge("mid", "sink"),
			},
			wantSum: "Invalid node type for a middle node",
		},
		{
			name: "branch does not reach an output",
			nodes: []ResourcePipelineNode{
				graphNode("in", "input"),
				graphNode("dead-end", "transform"),
			},
			edges:   []ResourcePipelineEdge{graphEdge("in", "dead-end")},
			wantSum: "Node does not lead to an output",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := validateGraph(tc.nodes, tc.edges)
			require.True(t, resp.Diagnostics.HasError(), "expected an error, got none")
			assert.Contains(t, summaries(resp), tc.wantSum)
		})
	}
}

// Enrichments are valid middle nodes, so a pipeline routing through one must
// pass — core's validMiddleNodeTypes is {transform, enrichment}.
func TestValidatePipelineGraphAcceptsEnrichmentMiddleNode(t *testing.T) {
	resp := validateGraph(
		[]ResourcePipelineNode{
			graphNode("in", "input"),
			graphNode("enrich", "enrichment"),
			graphNode("sink", "output"),
		},
		[]ResourcePipelineEdge{
			graphEdge("in", "enrich"),
			graphEdge("enrich", "sink"),
		},
	)
	assert.False(t, resp.Diagnostics.HasError(), "enrichment middle node must validate: %v", summaries(resp))
}

// A node whose slug is omitted is legal (the server generates one) and must not
// be reported, as long as no edge tries to reference it.
func TestValidatePipelineGraphToleratesOmittedSlug(t *testing.T) {
	nodes := []ResourcePipelineNode{
		graphNode("in", "input"),
		graphNode("sink", "output"),
		{
			ComponentType: types.StringValue("transform"),
			ComponentID:   types.StringValue("id-unreferenced"),
			Slug:          types.StringNull(),
		},
	}
	resp := validateGraph(nodes, []ResourcePipelineEdge{graphEdge("in", "sink")})
	for _, s := range summaries(resp) {
		assert.NotEqual(t, "Duplicate node slug", s)
		assert.NotEqual(t, "Unknown node slug", s)
	}
}

func TestFindPipelineCycle(t *testing.T) {
	outgoing := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	assert.NotEmpty(t, findPipelineCycle([]string{"a", "b", "c"}, outgoing))

	acyclic := map[string][]string{
		"in":  {"mid"},
		"mid": {"sink"},
	}
	assert.Empty(t, findPipelineCycle([]string{"in", "mid", "sink"}, acyclic))
}
