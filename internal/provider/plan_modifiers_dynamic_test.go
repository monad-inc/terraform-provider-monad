package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustDynamic(t *testing.T, v map[string]any) types.Dynamic {
	t.Helper()
	d, err := AnyToDynamic(v)
	require.NoError(t, err)
	return d
}

// TestDynamicConfigSemanticEqual is the ENG-9263 fix: on the first plan after
// import, a transform config that differs from the API value only by an
// empty-string field the API drops must not diff, while a genuine change must.
func TestDynamicConfigSemanticEqual(t *testing.T) {
	ctx := context.Background()
	m := dynamicConfigSemanticEqual{}

	// State from the API (import): no `description`. Config: explicit "".
	state := mustDynamic(t, map[string]any{
		"operations": []any{map[string]any{
			"operation": "drop_key",
			"arguments": map[string]any{"key": "x"},
		}},
	})
	config := mustDynamic(t, map[string]any{
		"operations": []any{map[string]any{
			"operation":   "drop_key",
			"description": "",
			"arguments":   map[string]any{"key": "x"},
		}},
	})

	t.Run("semantically equal -> plan pinned to state (no diff)", func(t *testing.T) {
		resp := &planmodifier.DynamicResponse{PlanValue: config}
		m.PlanModifyDynamic(ctx, planmodifier.DynamicRequest{
			StateValue: state, ConfigValue: config, PlanValue: config,
		}, resp)
		assert.True(t, resp.PlanValue.Equal(state), "expected plan value pinned to state")
	})

	t.Run("genuine change -> plan left unchanged", func(t *testing.T) {
		changed := mustDynamic(t, map[string]any{
			"operations": []any{map[string]any{
				"operation": "drop_key",
				"arguments": map[string]any{"key": "y"},
			}},
		})
		resp := &planmodifier.DynamicResponse{PlanValue: changed}
		m.PlanModifyDynamic(ctx, planmodifier.DynamicRequest{
			StateValue: state, ConfigValue: changed, PlanValue: changed,
		}, resp)
		assert.True(t, resp.PlanValue.Equal(changed), "expected plan unchanged for a genuine change")
		assert.False(t, resp.PlanValue.Equal(state))
	})

	t.Run("null state (create) -> no-op", func(t *testing.T) {
		resp := &planmodifier.DynamicResponse{PlanValue: config}
		m.PlanModifyDynamic(ctx, planmodifier.DynamicRequest{
			StateValue: types.DynamicNull(), ConfigValue: config, PlanValue: config,
		}, resp)
		assert.True(t, resp.PlanValue.Equal(config), "expected no-op when there is no prior state")
	})
}
