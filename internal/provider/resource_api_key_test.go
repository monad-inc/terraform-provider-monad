package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func apiKeySchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewResourceAPIKey().(*ResourceAPIKey).Schema(context.Background(), resource.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError(), "schema returned diagnostics: %v", resp.Diagnostics)
	return resp.Schema
}

// TestAPIKeyRoleIDRequiresReplace: the update endpoint can change only name and
// description, so a role change must plan as a replacement, not a doomed
// in-place update.
func TestAPIKeyRoleIDRequiresReplace(t *testing.T) {
	sa, ok := apiKeySchema(t).Attributes["role_id"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, sa.Required)
	assert.NotEmpty(t, sa.PlanModifiers, "role_id must carry RequiresReplace")
}

// TestAPIKeyTokenSensitiveComputed: the token is returned only by create and
// regenerate, so it must live in state, and must never print in plan output.
func TestAPIKeyTokenSensitiveComputed(t *testing.T) {
	sa, ok := apiKeySchema(t).Attributes["token"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, sa.Computed)
	assert.False(t, sa.Optional, "token is never set by the practitioner")
	assert.True(t, sa.Sensitive)
	assert.False(t, sa.WriteOnly, "write-only would discard the only copy of the token")
}

func TestSameInstant(t *testing.T) {
	s := types.StringValue
	assert.True(t, sameInstant(s("2027-01-01T00:00:00Z"), s("2027-01-01T00:00:00Z")))
	assert.True(t, sameInstant(s("2027-01-01T00:00:00Z"), s("2026-12-31T19:00:00-05:00")), "same moment, different offset")
	assert.True(t, sameInstant(s("2027-01-01T00:00:00Z"), s("2027-01-01T00:00:00.000Z")), "fractional zero")
	assert.False(t, sameInstant(s("2027-01-01T00:00:00Z"), s("2027-01-01T00:00:01Z")))
	assert.False(t, sameInstant(s("garbage"), s("2027-01-01T00:00:00Z")))
	assert.True(t, sameInstant(types.StringNull(), types.StringNull()))
	assert.False(t, sameInstant(types.StringNull(), s("2027-01-01T00:00:00Z")))
}

func TestReconcileTimestamp(t *testing.T) {
	// The API answers in UTC; the configured offset form is kept.
	got := reconcileTimestamp(types.StringValue("2026-12-31T19:00:00-05:00"), "2027-01-01T00:00:00Z")
	assert.Equal(t, "2026-12-31T19:00:00-05:00", got.ValueString())

	// A genuinely different time is adopted as drift.
	got = reconcileTimestamp(types.StringValue("2027-01-01T00:00:00Z"), "2028-01-01T00:00:00Z")
	assert.Equal(t, "2028-01-01T00:00:00Z", got.ValueString())

	// Import: no prior, the API value populates.
	got = reconcileTimestamp(types.StringNull(), "2027-01-01T00:00:00Z")
	assert.Equal(t, "2027-01-01T00:00:00Z", got.ValueString())
}

// TestAPIKeyModifyPlanToken: the token is planned unknown whenever the key may
// be regenerated. That includes an expiration_time known only at apply (e.g.
// from time_rotating): Terraform re-plans once the value is known, and a token
// that turned unknown only then failed as an inconsistent final plan, so such a
// key could never rotate.
func TestAPIKeyModifyPlanToken(t *testing.T) {
	ctx := context.Background()
	s := apiKeySchema(t)
	typ := s.Type().TerraformType(ctx).(tftypes.Object)

	const stored = "2099-01-01T00:00:00Z"
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	value := func(expiration, token tftypes.Value) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for name, at := range typ.AttributeTypes {
			vals[name] = tftypes.NewValue(at, nil)
		}
		vals["id"] = str("key-id")
		vals["name"] = str("ci")
		vals["role_id"] = str("role-id")
		vals["expiration_time"] = expiration
		vals["token"] = token
		return tftypes.NewValue(typ, vals)
	}

	cases := []struct {
		name        string
		state       tftypes.Value
		expiration  tftypes.Value
		planToken   tftypes.Value
		wantUnknown bool
	}{
		{"update with an expiration known only at apply", value(str(stored), str("old")), unknown, str("old"), true},
		{"update with a changed expiration", value(str(stored), str("old")), str("2099-06-01T00:00:00Z"), str("old"), true},
		{"update with the same instant keeps the token", value(str(stored), str("old")), str("2099-01-01T00:00:00+00:00"), str("old"), false},
		{"create with an expiration known only at apply", tftypes.NewValue(typ, nil), unknown, unknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// UseStateForUnknown has already run, so an update plan carries
			// state's token.
			plan := value(tc.expiration, tc.planToken)
			req := resource.ModifyPlanRequest{
				State: tfsdk.State{Schema: s, Raw: tc.state},
				Plan:  tfsdk.Plan{Schema: s, Raw: plan},
			}
			resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: plan}}

			(&ResourceAPIKey{}).ModifyPlan(ctx, req, &resp)
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

			var got types.String
			require.False(t, resp.Plan.GetAttribute(ctx, path.Root("token"), &got).HasError())
			assert.Equal(t, tc.wantUnknown, got.IsUnknown(), "token unknown")
			if !tc.wantUnknown {
				assert.Equal(t, "old", got.ValueString(), "token preserved from state")
			}
		})
	}
}
