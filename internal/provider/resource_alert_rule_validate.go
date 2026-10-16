package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithValidateConfig = &ResourceAlertRule{}

// billingAlertRuleTypes are the alert types the API refuses to create with
// API-key authentication. A billing account can span several organizations
// while an API key is scoped to one, so billing operations need a signed-in
// user. The provider only authenticates with an API key, so these always fail.
var billingAlertRuleTypes = map[string]bool{
	"billing-metrics-cost-budget": true,
}

// ValidateConfig rejects alert types the provider cannot create.
//
// Without it, a billing rule plans cleanly and then fails on apply with
// `400 billing alert rules cannot be created with API key authentication`.
// Catching it at plan tells the practitioner before anything is applied.
func (r *ResourceAlertRule) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var alertType types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("type"), &alertType)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validateAlertRuleType(alertType, resp)
}

func validateAlertRuleType(alertType types.String, resp *resource.ValidateConfigResponse) {
	// A value that comes from a variable or another resource is not knowable at
	// validate time; leave those to the API.
	if alertType.IsNull() || alertType.IsUnknown() {
		return
	}

	if billingAlertRuleTypes[alertType.ValueString()] {
		resp.Diagnostics.AddAttributeError(
			path.Root("type"),
			"Billing alert rules cannot be managed with Terraform",
			fmt.Sprintf(
				"%q is a billing alert type. A billing account can span several Monad "+
					"organizations, but an API key is scoped to a single organization, so the "+
					"Monad API does not accept billing operations from API keys. This provider "+
					"authenticates with an API key, so it cannot create this rule. Create billing "+
					"alert rules in the Monad UI and leave them out of your Terraform configuration.",
				alertType.ValueString(),
			),
		)
	}
}
