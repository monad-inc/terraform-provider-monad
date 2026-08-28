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

func alertRuleSchema(t *testing.T) schema.Schema {
	t.Helper()
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	NewResourceAlertRule().(*ResourceAlertRule).Schema(ctx, resource.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError(), "schema returned diagnostics: %v", resp.Diagnostics)
	return resp.Schema
}

// TestAlertRuleTypeRequiresReplace guards the immutability of `type`. The API
// rejects a type change on update (ENG-9549), so the attribute must carry a
// RequiresReplace plan modifier — otherwise Terraform would plan a doomed
// in-place update.
func TestAlertRuleTypeRequiresReplace(t *testing.T) {
	s := alertRuleSchema(t)

	attr, ok := s.Attributes["type"]
	require.True(t, ok, "type attribute missing")

	sa, ok := attr.(schema.StringAttribute)
	require.True(t, ok, "type is not a StringAttribute")
	assert.True(t, sa.Required, "type must be Required")
	assert.NotEmpty(t, sa.PlanModifiers, "type must carry a RequiresReplace plan modifier")
}

// TestAlertRuleActiveOptionalComputed pins `active` to the Optional+Computed
// shape (mirroring monad_pipeline.enabled) so an omitted attribute adopts the
// server default rather than showing a spurious diff on import.
func TestAlertRuleActiveOptionalComputed(t *testing.T) {
	s := alertRuleSchema(t)

	attr, ok := s.Attributes["active"]
	require.True(t, ok, "active attribute missing")

	ba, ok := attr.(schema.BoolAttribute)
	require.True(t, ok, "active is not a BoolAttribute")
	assert.True(t, ba.Optional, "active must be Optional")
	assert.True(t, ba.Computed, "active must be Computed (server default)")
}

// TestAlertRuleSeverityRequired pins `severity` to Required: the API rejects a
// create with no severity ("alert rule severity is required"), so the provider
// must surface that at plan time rather than as a 400 at apply (ENG-9549).
func TestAlertRuleSeverityRequired(t *testing.T) {
	s := alertRuleSchema(t)

	attr, ok := s.Attributes["severity"]
	require.True(t, ok, "severity attribute missing")

	sa, ok := attr.(schema.StringAttribute)
	require.True(t, ok, "severity is not a StringAttribute")
	assert.True(t, sa.Required, "severity must be Required (API rejects a missing severity)")
}

// TestAlertRulePipelineIDsOptional confirms pipeline_ids is optional so a rule
// with no pipelines (org-level alert types) is valid (ENG-9549).
func TestAlertRulePipelineIDsOptional(t *testing.T) {
	s := alertRuleSchema(t)

	attr, ok := s.Attributes["pipeline_ids"]
	require.True(t, ok, "pipeline_ids attribute missing")

	sa, ok := attr.(schema.SetAttribute)
	require.True(t, ok, "pipeline_ids is not a SetAttribute")
	assert.True(t, sa.Optional, "pipeline_ids must be Optional")
	assert.False(t, sa.Required, "pipeline_ids must not be Required")
}

// TestAlertRuleRuleConfigSemanticEqual confirms rule_config carries the shared
// dynamic semantic-equality plan modifier, giving a clean first plan after
// import the same way monad_transform.config does (ENG-9263).
func TestAlertRuleRuleConfigSemanticEqual(t *testing.T) {
	s := alertRuleSchema(t)

	attr, ok := s.Attributes["rule_config"]
	require.True(t, ok, "rule_config attribute missing")

	da, ok := attr.(schema.DynamicAttribute)
	require.True(t, ok, "rule_config is not a DynamicAttribute")
	require.True(t, da.Required, "rule_config must be Required")
	require.Len(t, da.PlanModifiers, 1, "rule_config must carry exactly one plan modifier")
	_, ok = da.PlanModifiers[0].(dynamicConfigSemanticEqual)
	assert.True(t, ok, "rule_config plan modifier must be dynamicConfigSemanticEqual")
}

// TestMaskServerInjectedKeys covers the billing-metrics case: the API injects
// settings.billing_account_id, which must be dropped from the comparison so it
// does not manufacture a perpetual diff (ENG-9549). Keys the practitioner
// authored are kept, and genuine value changes on those keys survive masking.
func TestMaskServerInjectedKeys(t *testing.T) {
	prior := map[string]any{
		"settings": map[string]any{"usd_amount": float64(5000)},
	}
	api := map[string]any{
		"settings": map[string]any{
			"usd_amount":         float64(5000),
			"billing_account_id": "6832",
		},
	}

	masked := maskServerInjectedKeys(prior, api)
	assert.Equal(t, map[string]any{
		"settings": map[string]any{"usd_amount": float64(5000)},
	}, masked, "server-injected billing_account_id must be dropped")

	// A genuine change to an authored key survives masking.
	apiDrift := map[string]any{
		"settings": map[string]any{
			"usd_amount":         float64(6000),
			"billing_account_id": "6832",
		},
	}
	maskedDrift := maskServerInjectedKeys(prior, apiDrift)
	assert.Equal(t, float64(6000), maskedDrift["settings"].(map[string]any)["usd_amount"],
		"a real change to usd_amount must not be masked away")
	_, hasInjected := maskedDrift["settings"].(map[string]any)["billing_account_id"]
	assert.False(t, hasInjected, "injected key still dropped even when another key drifts")
}

// TestReconcileAlertRuleConfig ties the masking to the reconcile: a billing
// rule whose only remote difference is the injected key keeps its prior state
// (clean plan), while a null prior (import) adopts the full API value.
func TestReconcileAlertRuleConfig(t *testing.T) {
	prior, err := AnyToDynamic(map[string]any{
		"settings": map[string]any{"usd_amount": float64(5000)},
	})
	require.NoError(t, err)

	api := map[string]any{
		"settings": map[string]any{
			"usd_amount":         float64(5000),
			"billing_account_id": "6832",
		},
	}

	got, err := reconcileAlertRuleConfig(prior, api)
	require.NoError(t, err)
	assert.True(t, got.Equal(prior), "injected billing_account_id must not churn state; got %v", got)

	// Import: null prior adopts the full API value (injected key included).
	imported, err := reconcileAlertRuleConfig(types.DynamicNull(), api)
	require.NoError(t, err)
	assert.False(t, imported.IsNull(), "import must populate rule_config from the API value")
}

func TestOptionalString(t *testing.T) {
	empty := ""
	value := "critical"

	assert.True(t, optionalString(nil).IsNull(), "nil must map to null")
	assert.True(t, optionalString(&empty).IsNull(), `"" must map to null (API omitempty)`)
	assert.Equal(t, types.StringValue("critical"), optionalString(&value))
}

// TestReconcileAlertRulePipelineIDs covers the null↔empty-set reconciliation:
// a rule that watches no pipelines keeps a null prior (no churn), while a
// populated API set — or genuine drift — repopulates state.
func TestReconcileAlertRulePipelineIDs(t *testing.T) {
	ctx := context.Background()

	nullSet := types.SetNull(types.StringType)
	twoSet, d := types.SetValueFrom(ctx, types.StringType, []string{"a", "b"})
	require.False(t, d.HasError())

	t.Run("empty API + null prior stays null", func(t *testing.T) {
		got, diags := reconcileAlertRulePipelineIDs(ctx, nullSet, nil)
		require.False(t, diags.HasError())
		assert.True(t, got.IsNull(), "expected null preserved")
	})

	t.Run("populated API repopulates", func(t *testing.T) {
		got, diags := reconcileAlertRulePipelineIDs(ctx, nullSet, []string{"a", "b"})
		require.False(t, diags.HasError())
		assert.True(t, got.Equal(twoSet), "expected set {a,b}, got %v", got)
	})

	t.Run("drift is reflected", func(t *testing.T) {
		got, diags := reconcileAlertRulePipelineIDs(ctx, twoSet, []string{"a"})
		require.False(t, diags.HasError())
		oneSet, _ := types.SetValueFrom(ctx, types.StringType, []string{"a"})
		assert.True(t, got.Equal(oneSet), "expected set {a}, got %v", got)
	})
}
