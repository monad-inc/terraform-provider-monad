package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithValidateConfig = &ResourcePipeline{}

// ValidateConfig checks each edge-condition rule against its rule's schema, and
// that every element is a well-formed rule or group.
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
		for j, element := range edge.Condition.Conditions {
			elementPath := path.Root("edges").
				AtListIndex(i).
				AtName("condition").
				AtName("conditions").
				AtListIndex(j)

			validateConditionElement(element, elementPath, resp)
		}
	}
}

// validateConditionElement checks that an element is either a rule or a group,
// not both, and validates the rules it holds.
func validateConditionElement(
	element ResourcePipelineConditionCondition,
	elementPath path.Path,
	resp *resource.ValidateConfigResponse,
) {
	// Values from a variable or another resource are not knowable at validate
	// time; leave those to the API.
	if element.Operator.IsUnknown() || element.TypeID.IsUnknown() {
		return
	}

	if !isConditionGroup(element) {
		if len(element.Conditions) > 0 {
			resp.Diagnostics.AddAttributeError(
				elementPath.AtName("conditions"),
				"Nested conditions need an operator",
				"Only a group can contain `conditions`. Set `operator` (and drop `type_id` and "+
					"`config`) to make this element a group of the rules inside it.",
			)
		}
		validateConditionLeaf(element.TypeID, element.Config, elementPath.AtName("config"), resp)
		return
	}

	if !element.TypeID.IsNull() && element.TypeID.ValueString() != "" {
		resp.Diagnostics.AddAttributeError(
			elementPath.AtName("type_id"),
			"Condition is both a rule and a group",
			"Set `type_id` (with `config`) for a rule, or `operator` (with nested `conditions`) "+
				"for a group, not both.",
		)
		return
	}
	if element.Config != nil && conditionConfigHasAnyField(*element.Config) {
		resp.Diagnostics.AddAttributeError(
			elementPath.AtName("config"),
			"Condition group takes no config",
			"A group combines its nested `conditions` with `operator`; settings belong on "+
				"each rule inside it.",
		)
	}

	operator := element.Operator.ValueString()
	if len(element.Conditions) == 0 && operator != "always" && operator != "never" {
		resp.Diagnostics.AddAttributeError(
			elementPath.AtName("conditions"),
			"Empty condition group",
			fmt.Sprintf("A group with operator %q needs at least one rule in `conditions`.", operator),
		)
	}

	for k, rule := range element.Conditions {
		validateConditionLeaf(rule.TypeID, rule.Config, elementPath.AtName("conditions").AtListIndex(k).AtName("config"), resp)
	}
}

// conditionConfigHasAnyField reports whether any config field is set.
func conditionConfigHasAnyField(cfg ResourcePipelineConditionConditionConfig) bool {
	for _, field := range allConditionFields {
		if _, set := conditionFieldValue(cfg, field); set {
			return true
		}
	}
	return false
}

func validateConditionLeaf(
	leafTypeID types.String,
	cfgPtr *ResourcePipelineConditionConditionConfig,
	configPath path.Path,
	resp *resource.ValidateConfigResponse,
) {
	typeIDPath := configPath.ParentPath().AtName("type_id")
	cfg := conditionConfigOrEmpty(cfgPtr)

	if leafTypeID.IsUnknown() {
		return
	}

	if leafTypeID.IsNull() || leafTypeID.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			typeIDPath,
			"Missing condition type_id",
			"Every condition rule needs a `type_id` naming the rule to evaluate "+
				"(for example \"equals\", \"equals_any\", \"key_exists\"); to group rules "+
				"instead, set `operator` and nest them in `conditions`. A rule without a "+
				"type_id is not evaluated and the edge routes nothing.",
		)
		return
	}

	typeID := leafTypeID.ValueString()
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
		if _, set := conditionFieldValue(cfg, field); !set {
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
		if _, set := conditionFieldValue(cfg, field); set {
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
