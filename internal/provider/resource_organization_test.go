package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgNamePrefix(t *testing.T) {
	assert.Equal(t, "acme", orgNamePrefix("acme-161d"))
	assert.Equal(t, "my-team", orgNamePrefix("my-team-0a9f"))
	assert.Equal(t, "no-suffix", orgNamePrefix("no-suffix"), "a name without the 4-hex suffix is returned as-is")
	assert.Equal(t, "upper-ABCD", orgNamePrefix("upper-ABCD"), "only lowercase hex is the API's suffix")
}

func orgSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewResourceOrganization().(*ResourceOrganization).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError())
	return resp.Schema
}

// TestOrganizationSchemaGuards pins the two guards on a destructive resource:
// a name change replaces (the API cannot rename), and deletion_protection is
// on unless switched off.
func TestOrganizationSchemaGuards(t *testing.T) {
	s := orgSchema(t)
	name := s.Attributes["name"].(schema.StringAttribute)
	assert.True(t, name.Required)
	assert.NotEmpty(t, name.PlanModifiers, "name must carry RequiresReplace")

	dp := s.Attributes["deletion_protection"].(schema.BoolAttribute)
	assert.True(t, dp.Optional && dp.Computed)
	require.NotNil(t, dp.Default)
}

// TestOrganizationDeleteRefusedWhenProtected: Delete must fail without calling
// the API while deletion_protection is true (the client is nil here, so any
// API call would panic).
func TestOrganizationDeleteRefusedWhenProtected(t *testing.T) {
	ctx := context.Background()
	s := orgSchema(t)
	typ := s.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for k, at := range typ.AttributeTypes {
		vals[k] = tftypes.NewValue(at, nil)
	}
	vals["id"] = tftypes.NewValue(tftypes.String, "org-id")
	vals["slug"] = tftypes.NewValue(tftypes.String, "acme-161d")
	vals["deletion_protection"] = tftypes.NewValue(tftypes.Bool, true)

	r := &ResourceOrganization{}
	resp := &resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(typ, vals)}}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Equal(t, "Sub-organization is deletion-protected", resp.Diagnostics.Errors()[0].Summary())
}

func TestOrganizationModifyPlanRefusesProtectedDestroyOrReplace(t *testing.T) {
	ctx := context.Background()
	s := orgSchema(t)
	typ := s.Type().TerraformType(ctx).(tftypes.Object)
	obj := func(name string, protected bool) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for k, at := range typ.AttributeTypes {
			vals[k] = tftypes.NewValue(at, nil)
		}
		vals["id"] = tftypes.NewValue(tftypes.String, "org-id")
		vals["name"] = tftypes.NewValue(tftypes.String, name)
		vals["slug"] = tftypes.NewValue(tftypes.String, name+"-161d")
		vals["deletion_protection"] = tftypes.NewValue(tftypes.Bool, protected)
		return tftypes.NewValue(typ, vals)
	}
	null := tftypes.NewValue(typ, nil)

	cases := []struct {
		name    string
		state   tftypes.Value
		plan    tftypes.Value
		wantErr bool
	}{
		{"destroy protected", obj("acme", true), null, true},
		{"replace protected", obj("acme", true), obj("acme2", true), true},
		{"replace while unprotecting in same plan", obj("acme", true), obj("acme2", false), true},
		{"in-place update protected", obj("acme", true), obj("acme", true), false},
		{"destroy unprotected", obj("acme", false), null, false},
		{"create", null, obj("acme", true), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ResourceOrganization{}
			resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: tc.plan}}
			r.ModifyPlan(ctx, resource.ModifyPlanRequest{
				State: tfsdk.State{Schema: s, Raw: tc.state},
				Plan:  tfsdk.Plan{Schema: s, Raw: tc.plan},
			}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
		})
	}
}
