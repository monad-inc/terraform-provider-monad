package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
)

// rawPresent / rawAbsent stand in for a whole-resource state or plan: the
// modifier only asks whether one exists (create and destroy are not changes).
var (
	rawPresent = tftypes.NewValue(tftypes.String, "present")
	rawAbsent  = tftypes.NewValue(tftypes.String, nil)
)

func TestImmutableNoReplaceString(t *testing.T) {
	cases := []struct {
		name          string
		state, plan   tftypes.Value
		stateV, planV types.String
		wantErr       bool
	}{
		{"create", rawAbsent, rawPresent, types.StringNull(), types.StringValue("a"), false},
		{"destroy", rawPresent, rawAbsent, types.StringValue("a"), types.StringNull(), false},
		{"unchanged", rawPresent, rawPresent, types.StringValue("a"), types.StringValue("a"), false},
		{"changed", rawPresent, rawPresent, types.StringValue("a"), types.StringValue("b"), true},
		{"unknown", rawPresent, rawPresent, types.StringValue("a"), types.StringUnknown(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &planmodifier.StringResponse{PlanValue: tc.planV}
			immutableNoReplace{reason: "why"}.PlanModifyString(context.Background(), planmodifier.StringRequest{
				Path:       path.Root("saml").AtName("entity_id"),
				State:      tfsdk.State{Raw: tc.state},
				Plan:       tfsdk.Plan{Raw: tc.plan},
				StateValue: tc.stateV,
				PlanValue:  tc.planV,
			}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
			assert.False(t, resp.RequiresReplace, "must never plan a replacement")
		})
	}
}

func TestImmutableNoReplaceSet(t *testing.T) {
	set := func(vs ...string) types.Set {
		elems := make([]attr.Value, len(vs))
		for i, v := range vs {
			elems[i] = types.StringValue(v)
		}
		return types.SetValueMust(types.StringType, elems)
	}
	nullSet := types.SetNull(types.StringType)
	cases := []struct {
		name         string
		stateV, plan types.Set
		wantErr      bool
	}{
		{"reordered is unchanged", set("a.com", "b.com"), set("b.com", "a.com"), false},
		{"domain added", set("a.com"), set("a.com", "b.com"), true},
		{"domains removed", set("a.com"), nullSet, true},
		{"domains set on a connection without any", nullSet, set("a.com"), true},
		{"unknown", set("a.com"), types.SetUnknown(types.StringType), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &planmodifier.SetResponse{PlanValue: tc.plan}
			immutableNoReplace{reason: "why"}.PlanModifySet(context.Background(), planmodifier.SetRequest{
				Path:       path.Root("email_domains"),
				State:      tfsdk.State{Raw: rawPresent},
				Plan:       tfsdk.Plan{Raw: rawPresent},
				StateValue: tc.stateV,
				PlanValue:  tc.plan,
			}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
			assert.False(t, resp.RequiresReplace, "must never plan a replacement")
		})
	}
}
