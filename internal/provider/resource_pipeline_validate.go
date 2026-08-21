package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ resource.ResourceWithValidateConfig = &ResourcePipeline{}

// ValidateConfig checks each edge-condition leaf against its rule's schema.
//
// The API accepts a leaf whose config does not match its type_id and then
// routes nothing at runtime — no error at save, none in the node logs, none on
// the edge (ENG-9546). Catching it at plan turns that silent data-routing
// failure into a message the practitioner sees before anything is applied.
// ENG-9345 tracks the server-side half; this is the client-side complement.
func (r *ResourcePipeline) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var data ResourcePipelineModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for i, edge := range data.Edges {
		for j, leaf := range edge.Condition.Conditions {
			configPath := path.Root("edges").
				AtListIndex(i).
				AtName("condition").
				AtName("conditions").
				AtListIndex(j).
				AtName("config")

			validateConditionLeaf(leaf, configPath, resp)
		}
	}
}

func validateConditionLeaf(
	leaf ResourcePipelineConditionCondition,
	configPath path.Path,
	resp *resource.ValidateConfigResponse,
) {
	typeIDPath := configPath.ParentPath().AtName("type_id")

	// A value that comes from a variable or another resource is not knowable at
	// validate time; leave those to the API.
	if leaf.TypeID.IsUnknown() {
		return
	}

	if leaf.TypeID.IsNull() || leaf.TypeID.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			typeIDPath,
			"Missing condition type_id",
			"Every condition leaf needs a `type_id` naming the rule to evaluate "+
				"(for example \"equals\", \"equals_any\", \"key_exists\"). A leaf without one "+
				"is not evaluated and the edge routes nothing.",
		)
		return
	}

	typeID := leaf.TypeID.ValueString()
	spec, known := conditionRules[typeID]
	if !known {
		// Warn rather than error: a rule shipped by the API after this provider
		// was built should not break an otherwise valid configuration.
		resp.Diagnostics.AddAttributeWarning(
			typeIDPath,
			"Unrecognized condition type_id",
			fmt.Sprintf(
				"%q is not a rule this provider knows about, so its configuration cannot be "+
					"checked here. Known rules: %s. If the rule is new, this warning is safe to "+
					"ignore — every field you set will be sent as-is.",
				typeID, strings.Join(knownRuleNames(), ", "),
			),
		)
		return
	}

	for _, field := range spec.required {
		if _, set := conditionFieldValue(leaf.Config, field); !set {
			resp.Diagnostics.AddAttributeError(
				configPath.AtName(field),
				"Missing required condition field",
				fmt.Sprintf(
					"type_id %q requires %q. %s",
					typeID, field, fieldHint(typeID, field),
				),
			)
		}
	}

	for _, field := range allConditionFields {
		if spec.allows(field) {
			continue
		}
		if _, set := conditionFieldValue(leaf.Config, field); set {
			resp.Diagnostics.AddAttributeError(
				configPath.AtName(field),
				"Condition field not used by this rule",
				fmt.Sprintf(
					"type_id %q does not read %q, so setting it has no effect. %s",
					typeID, field, fieldHint(typeID, field),
				),
			)
		}
	}
}

// allConditionFields is every field the config block exposes, used to detect a
// field set on a rule that ignores it.
var allConditionFields = []string{
	fieldKey, fieldValue, fieldValues, fieldPattern, fieldPercent, fieldRate,
	fieldNot, fieldCaseInsensitive, fieldRaw, fieldNull, fieldWhitespaceString,
}

// fieldHint points at the right field for the mistakes that are easy to make:
// value vs values, and the rules that take neither.
func fieldHint(typeID, field string) string {
	switch {
	case typeID == "equals_any" && field == fieldValues:
		return "Use values = [\"a\", \"b\"] for equals_any; `value` is for the single-value rules."
	case typeID == "equals_any" && field == fieldValue:
		return "equals_any matches a set — use `values` instead."
	case field == fieldValue && typeID == "matches_regex":
		return "matches_regex takes `pattern`, not `value`."
	case field == fieldPattern:
		return "Only matches_regex reads `pattern`."
	case field == fieldPercent:
		return "Only sample reads `percent`; for example percent = 12.5."
	case field == fieldRate:
		return "`rate` belongs to the deprecated sample_rate rule. Prefer type_id = \"sample\" with `percent`."
	case field == fieldValue:
		return "Single-value rules are equals, contains, starts_with, ends_with, greater_than and less_than."
	case field == fieldCaseInsensitive:
		return "case_insensitive applies to equals, equals_any, contains, starts_with and ends_with."
	case field == fieldRaw:
		return "`raw` applies to contains only."
	case field == fieldNull || field == fieldWhitespaceString:
		return "This flag applies to is_empty only."
	case field == fieldNot:
		return "`not` is accepted by every rule except sample."
	case field == fieldKey:
		return "`key` names the record field to test."
	}
	return ""
}

func knownRuleNames() []string {
	names := make([]string, 0, len(conditionRules))
	for name := range conditionRules {
		if conditionRules[name].deprecated {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
