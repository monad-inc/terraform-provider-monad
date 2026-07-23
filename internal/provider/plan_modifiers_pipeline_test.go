package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

func testEdge(name, from, to, op string) ResourcePipelineEdge {
	n := types.StringNull()
	if name != "" {
		n = types.StringValue(name)
	}
	return ResourcePipelineEdge{
		Name:                 n,
		Description:          types.StringNull(),
		FromNodeInstanceSlug: types.StringValue(from),
		ToNodeInstanceSlug:   types.StringValue(to),
		Condition:            ResourcePipelineCondition{Operator: types.StringValue(op)},
	}
}

func testNode(ctype, cid, slug string) ResourcePipelineNode {
	s := types.StringNull()
	if slug != "" {
		s = types.StringValue(slug)
	}
	return ResourcePipelineNode{
		ComponentType: types.StringValue(ctype),
		ComponentID:   types.StringValue(cid),
		Slug:          s,
	}
}

// TestPipelineEdgesSetEqual is the core of the import-diff fix (ENG-9221):
// edges that differ only in order must compare equal so the first plan after
// import is clean, while a genuine change must still be detected.
func TestPipelineEdgesSetEqual(t *testing.T) {
	a := testEdge("", "in", "t1", "always")
	b := testEdge("", "t1", "t2", "always")
	c := testEdge("", "t2", "sink", "always")

	t.Run("same set, different order -> equal", func(t *testing.T) {
		assert.True(t, pipelineEdgesSetEqual(
			[]ResourcePipelineEdge{a, b, c},
			[]ResourcePipelineEdge{c, a, b},
		))
	})

	t.Run("identical order -> equal", func(t *testing.T) {
		assert.True(t, pipelineEdgesSetEqual(
			[]ResourcePipelineEdge{a, b, c},
			[]ResourcePipelineEdge{a, b, c},
		))
	})

	t.Run("null name vs empty-string name -> equal", func(t *testing.T) {
		nullName := testEdge("", "in", "t1", "always")
		emptyName := testEdge("", "in", "t1", "always")
		emptyName.Name = types.StringValue("")
		assert.True(t, pipelineEdgesSetEqual(
			[]ResourcePipelineEdge{nullName},
			[]ResourcePipelineEdge{emptyName},
		))
	})

	t.Run("changed edge target -> not equal", func(t *testing.T) {
		changed := testEdge("", "t2", "other-sink", "always")
		assert.False(t, pipelineEdgesSetEqual(
			[]ResourcePipelineEdge{a, b, c},
			[]ResourcePipelineEdge{a, b, changed},
		))
	})

	t.Run("different count -> not equal", func(t *testing.T) {
		assert.False(t, pipelineEdgesSetEqual(
			[]ResourcePipelineEdge{a, b, c},
			[]ResourcePipelineEdge{a, b},
		))
	})

	t.Run("set name difference -> not equal", func(t *testing.T) {
		named := testEdge("edge-name", "in", "t1", "always")
		assert.False(t, pipelineEdgesSetEqual(
			[]ResourcePipelineEdge{a},
			[]ResourcePipelineEdge{named},
		))
	})
}

func TestPipelineNodesSetEqual(t *testing.T) {
	in := testNode("input", "id-in", "cloudtrail-input")
	t1 := testNode("transform", "id-t1", "drop-low-value-fields")
	sink := testNode("output", "id-sink", "sink")

	t.Run("same set, different order -> equal", func(t *testing.T) {
		assert.True(t, pipelineNodesSetEqual(
			[]ResourcePipelineNode{in, t1, sink},
			[]ResourcePipelineNode{sink, in, t1},
		))
	})

	t.Run("null slug vs empty-string slug -> equal", func(t *testing.T) {
		nullSlug := testNode("input", "id-in", "")
		emptySlug := testNode("input", "id-in", "")
		emptySlug.Slug = types.StringValue("")
		assert.True(t, pipelineNodesSetEqual(
			[]ResourcePipelineNode{nullSlug},
			[]ResourcePipelineNode{emptySlug},
		))
	})

	t.Run("changed component id -> not equal", func(t *testing.T) {
		changed := testNode("output", "id-other", "sink")
		assert.False(t, pipelineNodesSetEqual(
			[]ResourcePipelineNode{in, t1, sink},
			[]ResourcePipelineNode{in, t1, changed},
		))
	})
}
