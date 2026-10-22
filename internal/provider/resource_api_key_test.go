package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
