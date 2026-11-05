package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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

// TestStorageCostRatesValidateConfig pins the plan-time checks from the #47
// review: an empty configuration (which Read would take for an import and an
// apply would zero every rate in the organization), a price finer than the
// API's DECIMAL(10, 6), and an output_costs key that is not an output ID (the
// API writes the costs entries first, then fails with a 500).
func TestStorageCostRatesValidateConfig(t *testing.T) {
	ctx := context.Background()
	r := &ResourceStorageCostRates{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	s := sr.Schema
	typ := s.Type().TerraformType(ctx).(tftypes.Object)
	costsT := typ.AttributeTypes["costs"]
	outsT := typ.AttributeTypes["output_costs"]
	outT := outsT.(tftypes.Map).ElementType

	costs := func(kv map[string]float64) tftypes.Value {
		m := map[string]tftypes.Value{}
		for k, v := range kv {
			m[k] = tftypes.NewValue(tftypes.Number, v)
		}
		return tftypes.NewValue(costsT, m)
	}
	outs := func(id string, cost float64) tftypes.Value {
		return tftypes.NewValue(outsT, map[string]tftypes.Value{id: tftypes.NewValue(outT, map[string]tftypes.Value{
			"storage_type": tftypes.NewValue(tftypes.String, "dev-null"),
			"cost_per_gb":  tftypes.NewValue(tftypes.Number, cost),
		})})
	}
	cfg := func(c, o tftypes.Value) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for k, at := range typ.AttributeTypes {
			vals[k] = tftypes.NewValue(at, nil)
		}
		vals["costs"], vals["output_costs"] = c, o
		return tftypes.NewValue(typ, vals)
	}
	const outID = "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	nullCosts, nullOuts := tftypes.NewValue(costsT, nil), tftypes.NewValue(outsT, nil)

	cases := []struct {
		name    string
		config  tftypes.Value
		wantErr bool
	}{
		{"costs only", cfg(costs(map[string]float64{"dev-null": 0.5}), nullOuts), false},
		{"output_costs only", cfg(nullCosts, outs(outID, 0.25)), false},
		{"empty costs map is still configured", cfg(costs(map[string]float64{}), nullOuts), false},
		{"neither set", cfg(nullCosts, nullOuts), true},
		{"six decimals", cfg(costs(map[string]float64{"dev-null": 0.001235}), nullOuts), false},
		{"seven decimals", cfg(costs(map[string]float64{"dev-null": 0.0012345}), nullOuts), true},
		{"output seven decimals", cfg(nullCosts, outs(outID, 0.1234567)), true},
		{"out of range", cfg(costs(map[string]float64{"dev-null": 1000.5}), nullOuts), true},
		{"output key not a UUID", cfg(nullCosts, outs("my-splunk-output", 0.25)), true},
		// An entry built from a value known only at apply is itself unknown.
		{"output entry unknown", cfg(nullCosts, tftypes.NewValue(outsT, map[string]tftypes.Value{
			outID: tftypes.NewValue(outT, tftypes.UnknownValue),
		})), false},
		{"output cost unknown", cfg(nullCosts, tftypes.NewValue(outsT, map[string]tftypes.Value{outID: tftypes.NewValue(outT, map[string]tftypes.Value{
			"storage_type": tftypes.NewValue(tftypes.String, "dev-null"),
			"cost_per_gb":  tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		})})), false},
		{"costs value unknown", cfg(tftypes.NewValue(costsT, map[string]tftypes.Value{
			"dev-null": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		}), nullOuts), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: tc.config}}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
		})
	}
}

func TestCostDecimals(t *testing.T) {
	for f, want := range map[float64]int{0: 0, 12: 0, 0.5: 1, 0.023: 3, 0.001235: 6, 0.0012345678: 10, 999.999999: 6} {
		assert.Equal(t, want, costDecimals(f), "%v", f)
	}
}
