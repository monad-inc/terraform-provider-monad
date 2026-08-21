package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guard for ENG-9572.
//
// ENG-9221 attached order-insensitive plan modifiers to the pipeline `nodes`
// and `edges` list blocks. They set the planned value to the prior state
// whenever state and config held the same set in a different order, which is
// not a legal plan: Terraform requires a plan-known attribute to equal the
// config value at the same index. After `terraform import` — the very case
// they were written for — state carries API order while config carries the
// authored order, so every position where the two disagreed became
// "Provider produced invalid plan", and destroy was blocked with it.
//
// The premise cannot be satisfied for a List. When config order and state
// order differ, no single plan can be element-wise equal to config AND equal
// to state. So these blocks must carry no plan modifier that rewrites element
// order. If someone reattaches one, this fails.
func TestPipelineListBlocksHaveNoOrderRewritingPlanModifiers(t *testing.T) {
	ctx := context.Background()
	r := NewResourcePipeline()

	resp := &resource.SchemaResponse{}
	r.(*ResourcePipeline).Schema(ctx, resource.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError(), "schema returned diagnostics: %v", resp.Diagnostics)

	for _, name := range []string{"nodes", "edges"} {
		blk, ok := resp.Schema.Blocks[name]
		require.True(t, ok, "block %q missing from the pipeline schema", name)

		lb, ok := blk.(schema.ListNestedBlock)
		require.True(t, ok, "block %q is not a ListNestedBlock", name)

		assert.Empty(t, lb.PlanModifiers,
			"block %q must not carry a list plan modifier: rewriting element order "+
				"produces a plan that disagrees with config element-wise (ENG-9572)", name)
	}
}
