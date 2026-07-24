package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// dynamicConfigSemanticEqual keeps the prior state value for a Dynamic config
// attribute when the configured value is semantically equal to it — same data
// modulo empty/omitted fields and cty representation (the pruneEmpty-based
// comparison Read already uses via reconcileDynamic).
//
// This gives a clean first plan after `terraform import`: Read adopts the API
// value verbatim when prior state is null, and the API drops empty-string
// fields (omitempty) that the practitioner's config carries explicitly (e.g. a
// transform operation's `description = ""`), so without this the first plan
// spuriously re-adds them (ENG-9263). Genuine config changes still diff because
// the values are no longer semantically equal.
type dynamicConfigSemanticEqual struct{}

var _ planmodifier.Dynamic = dynamicConfigSemanticEqual{}

func (dynamicConfigSemanticEqual) Description(context.Context) string {
	return "Keeps prior state when the configured value is semantically equal (ignoring empty/omitted fields)."
}

func (m dynamicConfigSemanticEqual) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (dynamicConfigSemanticEqual) PlanModifyDynamic(
	ctx context.Context,
	req planmodifier.DynamicRequest,
	resp *planmodifier.DynamicResponse,
) {
	// Nothing to preserve on create (no prior state) or when values aren't
	// fully known yet.
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsUnknown() {
		return
	}

	stateMap, err := tfDynamicToMapAny(req.StateValue)
	if err != nil {
		return
	}
	configMap, err := tfDynamicToMapAny(req.ConfigValue)
	if err != nil {
		return
	}

	if dynamicsSemanticallyEqual(stateMap, configMap) {
		resp.PlanValue = req.StateValue
	}
}
