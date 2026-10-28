package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	monad "github.com/monad-inc/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

func testCatalog() []monad.ModelsPermission {
	p := func(id, slug string) monad.ModelsPermission {
		return monad.ModelsPermission{Id: monad.PtrString(id), Slug: monad.PtrString(slug)}
	}
	return []monad.ModelsPermission{p("id-pr", "pipeline:read"), p("id-ir", "input:read"), p("id-kw", "apikey:write")}
}

func TestResolvePermissionSlugs(t *testing.T) {
	ids, err := resolvePermissionSlugs(testCatalog(), []string{"input:read", "pipeline:read"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"id-ir", "id-pr"}, ids)

	// Every unknown slug is reported at once, with the valid ones.
	_, err = resolvePermissionSlugs(testCatalog(), []string{"pipeline:read", "pipline:read", "inputs:read"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown permission slugs: inputs:read, pipline:read")
	assert.Contains(t, err.Error(), "Valid permissions: apikey:write, input:read, pipeline:read")

	_, err = resolvePermissionSlugs(testCatalog(), []string{"nope"})
	assert.Contains(t, err.Error(), "unknown permission slug: nope")
}

func TestPermissionSlugs(t *testing.T) {
	assert.Equal(t, []string{"pipeline:read", "input:read", "apikey:write"}, permissionSlugs(testCatalog()))
	assert.Empty(t, permissionSlugs(nil))
}

// TestRolePermissionsIsRequiredSet: slugs, unordered, and required, because the
// API cannot reduce a role to zero permissions.
func TestRolePermissionsIsRequiredSet(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewResourceRole().(*ResourceRole).Schema(context.Background(), resource.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError())
	sa, ok := resp.Schema.Attributes["permissions"].(schema.SetAttribute)
	require.True(t, ok, "permissions must be a set")
	assert.True(t, sa.Required)
}

// roleModifyPlan runs ModifyPlan for a role whose state holds stateSlugs (nil
// for a create) and whose plan holds planSlugs, against a fake API that serves
// testCatalog. It returns the diagnostics and how many catalog requests were
// made.
func roleModifyPlan(t *testing.T, stateSlugs, planSlugs []string) (diag.Diagnostics, int32) {
	t.Helper()
	ctx := context.Background()

	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/roles/permissions") {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"permissions": testCatalog(),
			"pagination":  map[string]any{"total": len(testCatalog())},
		})
	}))
	t.Cleanup(srv.Close)

	r := &ResourceRole{client: client.NewMonadAPIClient(srv.URL, "token", "org", true, 0)}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	typ := schemaResp.Schema.Type().TerraformType(ctx)

	value := func(slugs []string) tftypes.Value {
		elems := make([]tftypes.Value, len(slugs))
		for i, s := range slugs {
			elems[i] = tftypes.NewValue(tftypes.String, s)
		}
		return tftypes.NewValue(typ, map[string]tftypes.Value{
			"timeouts":    tftypes.NewValue(typ.(tftypes.Object).AttributeTypes["timeouts"], nil),
			"id":          tftypes.NewValue(tftypes.String, "role-id"),
			"created_at":  tftypes.NewValue(tftypes.String, nil),
			"updated_at":  tftypes.NewValue(tftypes.String, nil),
			"name":        tftypes.NewValue(tftypes.String, "r"),
			"description": tftypes.NewValue(tftypes.String, nil),
			"permissions": tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, elems),
		})
	}

	state := tftypes.NewValue(typ, nil)
	if stateSlugs != nil {
		state = value(stateSlugs)
	}
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: schemaResp.Schema, Raw: value(planSlugs)}}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		State: tfsdk.State{Schema: schemaResp.Schema, Raw: state},
		Plan:  tfsdk.Plan{Schema: schemaResp.Schema, Raw: value(planSlugs)},
	}, resp)
	return resp.Diagnostics, calls.Load()
}

func TestRoleModifyPlanChecksSlugs(t *testing.T) {
	t.Run("unknown slug on create fails at plan", func(t *testing.T) {
		diags, calls := roleModifyPlan(t, nil, []string{"pipeline:read", "pipline:read"})
		require.True(t, diags.HasError())
		assert.Contains(t, diags.Errors()[0].Detail(), "unknown permission slug: pipline:read")
		assert.Equal(t, int32(1), calls)
	})

	t.Run("unknown slug added to an existing role fails at plan", func(t *testing.T) {
		diags, calls := roleModifyPlan(t, []string{"pipeline:read"}, []string{"pipeline:read", "inptu:read"})
		require.True(t, diags.HasError())
		assert.Equal(t, int32(1), calls)
	})

	t.Run("valid change passes", func(t *testing.T) {
		diags, calls := roleModifyPlan(t, []string{"pipeline:read"}, []string{"pipeline:read", "input:read"})
		assert.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, int32(1), calls)
	})

	t.Run("unchanged permissions do not read the catalog", func(t *testing.T) {
		diags, calls := roleModifyPlan(t, []string{"pipeline:read", "input:read"}, []string{"input:read", "pipeline:read"})
		assert.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, int32(0), calls)
	})
}
