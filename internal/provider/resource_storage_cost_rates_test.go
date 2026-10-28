package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	monad "github.com/monad-inc/sdk/go"
	"github.com/stretchr/testify/assert"
)

// The SDK carries cost_per_gb as float32; widening naively turns the API's
// 0.001 into 0.0010000000474974513 and every plan would show a diff.
func TestFloat32ToFloat64(t *testing.T) {
	for _, v := range []float64{0.001, 0.3, 1.25, 0.4, 1000, 0, 12.345} {
		assert.Equal(t, v, float32ToFloat64(float32(v)), "%v", v)
	}
	assert.True(t, sameCost(0.001, float32ToFloat64(float32(0.001))))
	assert.False(t, sameCost(0.3, 0.31))
}

func TestSplitCostEntries(t *testing.T) {
	f := func(v float32) *float32 { return &v }
	typesOut, outputs := splitCostEntries([]monad.ModelsStorageTypeCostEntry{
		{StorageType: monad.PtrString("dev-null"), CostPerGb: f(0.3)},
		{StorageType: monad.PtrString("kv-lookup"), CostPerGb: f(1)},
		{StorageType: monad.PtrString("dev-null"), CostPerGb: f(2.5), OutputId: monad.PtrString("out-1")},
	})
	assert.Equal(t, map[string]float64{"dev-null": 0.3, "kv-lookup": 1}, typesOut, "per-type defaults have no output_id")
	assert.Equal(t, map[string]outputCost{"out-1": {storageType: "dev-null", cost: 2.5}}, outputs)
}

func TestKeepNullIfEmpty(t *testing.T) {
	ctx := context.Background()
	var d diag.Diagnostics

	// Never configured, nothing managed: stays null (no spurious diff).
	assert.True(t, keepNullIfEmpty(ctx, types.MapNull(types.Float64Type), nil, false, &d).IsNull())
	// Import with nothing in the org: null.
	assert.True(t, keepNullIfEmpty(ctx, types.MapNull(types.Float64Type), map[string]float64{}, true, &d).IsNull())
	// Managed keys that all vanished: empty, so the plan re-adds them.
	prior := types.MapValueMust(types.Float64Type, map[string]attr.Value{"dev-null": types.Float64Value(0.3)})
	got := keepNullIfEmpty(ctx, prior, map[string]float64{}, false, &d)
	assert.False(t, got.IsNull())
	assert.Empty(t, got.Elements())
	assert.False(t, d.HasError())
}
