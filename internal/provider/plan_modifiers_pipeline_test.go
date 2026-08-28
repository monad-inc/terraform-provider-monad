package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guard for ENG-9573 (and, through it, ENG-9221 / ENG-9572).
//
// Pipeline topology is a graph: a node is identified by its slug, an edge by
// its from/to pair, and the position of either in the HCL file carries no
// meaning. Modelling `nodes`/`edges` as lists made index load-bearing, which
// produced a spurious reorder diff after `terraform import` (ENG-9221); the
// 0.3.0 attempt to hide that with an order-insensitive plan modifier pinned the
// plan to state order, violating Terraform's rule that a plan-known value equal
// config at the same index — every disagreeing position became "Provider
// produced invalid plan" and blocked destroy (ENG-9572).
//
// Sets are the durable fix: Terraform compares them by element value, not
// index, so a reorder is not a diff and no plan modifier is needed. If someone
// reverts these blocks to lists — or reattaches an order-rewriting plan
// modifier — this fails.
func TestPipelineNodesAndEdgesAreSetBlocks(t *testing.T) {
	ctx := context.Background()
	r := NewResourcePipeline()

	resp := &resource.SchemaResponse{}
	r.(*ResourcePipeline).Schema(ctx, resource.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError(), "schema returned diagnostics: %v", resp.Diagnostics)

	for _, name := range []string{"nodes", "edges"} {
		blk, ok := resp.Schema.Blocks[name]
		require.True(t, ok, "block %q missing from the pipeline schema", name)

		sb, ok := blk.(schema.SetNestedBlock)
		require.Truef(t, ok, "block %q must be a SetNestedBlock, not a %T: element order "+
			"is not semantically significant in a pipeline graph (ENG-9573)", blk, name)

		assert.Empty(t, sb.PlanModifiers,
			"block %q must not carry an order-rewriting plan modifier (ENG-9572)", name)
	}
}
