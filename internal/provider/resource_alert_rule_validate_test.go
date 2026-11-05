package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validateType(alertType types.String) *resource.ValidateConfigResponse {
	resp := &resource.ValidateConfigResponse{}
	validateAlertRuleType(alertType, resp)
	return resp
}

// TestValidateAlertRuleType pins the plan-time rejection of billing alert
// types. The API refuses billing operations from API keys, and the provider
// only authenticates with one, so a billing rule would otherwise plan cleanly
// and fail on apply.
func TestValidateAlertRuleType(t *testing.T) {
	t.Run("billing type errors on the type attribute", func(t *testing.T) {
		resp := validateType(types.StringValue("billing-metrics-cost-budget"))
		require.True(t, resp.Diagnostics.HasError())
		require.Len(t, resp.Diagnostics.Errors(), 1)

		d := resp.Diagnostics.Errors()[0]
		assert.Equal(t, "Billing alert rules cannot be managed with Terraform", d.Summary())
		assert.Contains(t, d.Detail(), "billing-metrics-cost-budget")
		assert.Contains(t, d.Detail(), "Monad UI")

		withPath, ok := d.(interface{ Path() path.Path })
		require.True(t, ok, "diagnostic should carry an attribute path")
		assert.True(t, withPath.Path().Equal(path.Root("type")))
	})

	for _, typ := range []string{
		"threshold-alert", "error-rate-alert", "pipeline-status-alert",
		"volume-anomaly-alert", "monad-log-alert",
	} {
		t.Run(typ+" passes", func(t *testing.T) {
			assert.False(t, validateType(types.StringValue(typ)).Diagnostics.HasError())
		})
	}

	t.Run("unknown type is left to the API", func(t *testing.T) {
		assert.Empty(t, validateType(types.StringUnknown()).Diagnostics)
	})

	t.Run("null type is left to the schema", func(t *testing.T) {
		assert.Empty(t, validateType(types.StringNull()).Diagnostics)
	})
}
