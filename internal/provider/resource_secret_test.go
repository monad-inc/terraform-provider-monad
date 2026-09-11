package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// TestReconcileOptionalString covers the ""/null reconciliation for Optional
// string scalars refreshed from the API (ENG-9867): the SDK getters yield "" for an
// unset description, but an omitted attribute is null in the plan, so Read
// must not store a value the plan never carried — while an explicit
// `description = ""` in config must keep round-tripping as "".
func TestReconcileOptionalString(t *testing.T) {
	const (
		empty = ""
		set   = "a description"
	)

	null := types.StringNull()
	blank := types.StringValue("")
	other := types.StringValue("old text")

	cases := []struct {
		name  string
		prior types.String
		api   string
		want  types.String
	}{
		{"empty, null prior", null, empty, null},
		{"empty, unknown prior (import)", types.StringUnknown(), empty, null},
		{"empty, explicit \"\" prior stays \"\"", blank, empty, blank},
		{"empty, prior had text (cleared out-of-band)", other, empty, null},
		{"set, null prior", null, set, types.StringValue(set)},
		{"set, prior had other text", other, set, types.StringValue(set)},
		{"set, prior blank", blank, set, types.StringValue(set)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, reconcileOptionalString(tc.prior, tc.api))
		})
	}
}

// secretValues builds a raw monad_secret object for tfsdk Config/State/Plan.
// value is write-only, so it is only ever non-null in config; valueHash is
// only ever set in state/plan.
func secretValues(t *testing.T, typ tftypes.Type, value, valueHash tftypes.Value) tftypes.Value {
	t.Helper()
	// The timeouts block is part of the object type; it is never set in these
	// cases, so it is a null object of the schema's own type.
	timeoutsType := typ.(tftypes.Object).AttributeTypes["timeouts"]
	return tftypes.NewValue(typ, map[string]tftypes.Value{
		"timeouts":    tftypes.NewValue(timeoutsType, nil),
		"id":          tftypes.NewValue(tftypes.String, "secret-id"),
		"name":        tftypes.NewValue(tftypes.String, "example"),
		"description": tftypes.NewValue(tftypes.String, nil),
		"value":       value,
		"value_hash":  valueHash,
	})
}

// TestSecretModifyPlanDetectsRotation covers the write-only `value` rotation
// detection (ENG-9235). `value` is null in state and plan, so only a plan-time
// comparison of the configured value's fingerprint against the stored
// `value_hash` can notice a change; on mismatch the hash is planned unknown so
// Update runs and can store the new hash without an apply-consistency error.
func TestSecretModifyPlanDetectsRotation(t *testing.T) {
	ctx := context.Background()
	r := &ResourceSecret{client: &client.Client{OrganizationID: "org"}}

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	require.False(t, schemaResp.Diagnostics.HasError(), schemaResp.Diagnostics)
	s := schemaResp.Schema
	typ := s.Type().TerraformType(ctx)

	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	null := tftypes.NewValue(tftypes.String, nil)
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

	storedHash := r.computeValueHash(ctx, "current-value")

	cases := []struct {
		name        string
		config      tftypes.Value // nil ⇒ whole object null (destroy)
		state       tftypes.Value // nil ⇒ whole object null (create)
		wantUnknown bool
	}{
		{
			name:        "unchanged value keeps the stored hash",
			config:      secretValues(t, typ, str("current-value"), null),
			state:       secretValues(t, typ, null, str(storedHash)),
			wantUnknown: false,
		},
		{
			name:        "rotated value marks the hash unknown",
			config:      secretValues(t, typ, str("new-value"), null),
			state:       secretValues(t, typ, null, str(storedHash)),
			wantUnknown: true,
		},
		{
			name:        "value unknown until apply marks the hash unknown",
			config:      secretValues(t, typ, unknown, null),
			state:       secretValues(t, typ, null, str(storedHash)),
			wantUnknown: true,
		},
		{
			name:        "missing stored hash marks the hash unknown",
			config:      secretValues(t, typ, str("current-value"), null),
			state:       secretValues(t, typ, null, null),
			wantUnknown: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The framework's UseStateForUnknown has already run by the time
			// ModifyPlan is called, so the incoming plan carries state's hash.
			var planHash tftypes.Value
			if err := tc.state.As(&map[string]tftypes.Value{}); err == nil {
				var m map[string]tftypes.Value
				require.NoError(t, tc.state.As(&m))
				planHash = m["value_hash"]
			}
			plan := secretValues(t, typ, null, planHash)

			req := resource.ModifyPlanRequest{
				Config: tfsdk.Config{Schema: s, Raw: tc.config},
				State:  tfsdk.State{Schema: s, Raw: tc.state},
				Plan:   tfsdk.Plan{Schema: s, Raw: plan},
			}
			resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: plan}}

			r.ModifyPlan(ctx, req, &resp)
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

			var got types.String
			require.False(t, resp.Plan.GetAttribute(ctx, path.Root("value_hash"), &got).HasError())
			assert.Equal(t, tc.wantUnknown, got.IsUnknown(), "value_hash unknown")
			if !tc.wantUnknown {
				assert.Equal(t, storedHash, got.ValueString(), "hash preserved from state")
			}
		})
	}
}

// TestSecretModifyPlanNoopOnCreateAndDestroy asserts the rotation check stays
// out of the way when there is nothing to compare: a create has no prior
// state (the hash is computed in Create) and a destroy has no plan.
func TestSecretModifyPlanNoopOnCreateAndDestroy(t *testing.T) {
	ctx := context.Background()
	r := &ResourceSecret{client: &client.Client{OrganizationID: "org"}}

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	typ := s.Type().TerraformType(ctx)

	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	null := tftypes.NewValue(tftypes.String, nil)
	nullObj := tftypes.NewValue(typ, nil)
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

	t.Run("create", func(t *testing.T) {
		plan := secretValues(t, typ, null, unknown)
		req := resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: s, Raw: secretValues(t, typ, str("v"), null)},
			State:  tfsdk.State{Schema: s, Raw: nullObj},
			Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		}
		resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: plan}}
		r.ModifyPlan(ctx, req, &resp)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, resp.Plan.Raw.Equal(plan), "plan untouched on create")
	})

	t.Run("destroy", func(t *testing.T) {
		req := resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: s, Raw: nullObj},
			State:  tfsdk.State{Schema: s, Raw: secretValues(t, typ, null, str("h"))},
			Plan:   tfsdk.Plan{Schema: s, Raw: nullObj},
		}
		resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: nullObj}}
		r.ModifyPlan(ctx, req, &resp)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, resp.Plan.Raw.IsNull(), "plan untouched on destroy")
	})
}
