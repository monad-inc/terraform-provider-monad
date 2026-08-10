package provider

import (
	"encoding/json"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Edge-condition leaf rules and the config fields each one reads.
//
// This table is the single source of truth for three things that must agree or
// the failure is silent: what buildPipelineRequestEdges serializes, what
// buildPipelineStateEdges reads back, and what ValidateConfig accepts. Before
// ENG-9546 the provider emitted {key, value, rate} for every rule regardless of
// type_id, so nine of the eleven rules were unusable — the API compared a
// record's "hot" against the literal text ["hot"] and routed nothing, with no
// error at plan, apply, or runtime.
//
// Field names match the API's rule catalogue (GET /v3/pipeline_edges/edge_condition_rules)
// exactly, so a field here is the JSON key sent to the API.
const (
	fieldKey              = "key"
	fieldValue            = "value"
	fieldValues           = "values"
	fieldPattern          = "pattern"
	fieldPercent          = "percent"
	fieldRate             = "rate"
	fieldNot              = "not"
	fieldCaseInsensitive  = "case_insensitive"
	fieldRaw              = "raw"
	fieldNull             = "null"
	fieldWhitespaceString = "whitespace_string"
)

type ruleSpec struct {
	// required fields must be set for the rule to function. The API accepts a
	// leaf without them and then routes nothing, which is why these are checked
	// at plan time instead.
	required []string
	optional []string
	// deprecated marks a rule the API still honors but no longer advertises in
	// its catalogue.
	deprecated bool
}

func (r ruleSpec) allows(field string) bool {
	for _, f := range r.required {
		if f == field {
			return true
		}
	}
	for _, f := range r.optional {
		if f == field {
			return true
		}
	}
	return false
}

func (r ruleSpec) fields() []string {
	out := make([]string, 0, len(r.required)+len(r.optional))
	out = append(out, r.required...)
	return append(out, r.optional...)
}

// conditionRules mirrors the API's rule catalogue. greater_than / less_than
// take a numeric value, but the API's InitKeyNumeric parses a numeric *string*
// via strconv.ParseFloat, so a single string-typed `value` covers every
// value-taking rule and round-trips unchanged.
var conditionRules = map[string]ruleSpec{
	"equals": {
		required: []string{fieldKey, fieldValue},
		optional: []string{fieldCaseInsensitive, fieldNot},
	},
	"contains": {
		required: []string{fieldKey, fieldValue},
		optional: []string{fieldCaseInsensitive, fieldRaw, fieldNot},
	},
	"starts_with": {
		required: []string{fieldKey, fieldValue},
		optional: []string{fieldCaseInsensitive, fieldNot},
	},
	"ends_with": {
		required: []string{fieldKey, fieldValue},
		optional: []string{fieldCaseInsensitive, fieldNot},
	},
	"greater_than": {
		required: []string{fieldKey, fieldValue},
		optional: []string{fieldNot},
	},
	"less_than": {
		required: []string{fieldKey, fieldValue},
		optional: []string{fieldNot},
	},
	"equals_any": {
		required: []string{fieldKey, fieldValues},
		optional: []string{fieldCaseInsensitive, fieldNot},
	},
	"matches_regex": {
		required: []string{fieldKey, fieldPattern},
		optional: []string{fieldNot},
	},
	"sample": {
		required: []string{fieldPercent},
		optional: []string{fieldKey},
	},
	"key_exists": {
		required: []string{fieldKey},
		optional: []string{fieldNot},
	},
	"is_empty": {
		required: []string{fieldKey},
		optional: []string{fieldNull, fieldWhitespaceString, fieldNot},
	},
	// sample_rate predates `sample` and is not in the API's advertised
	// catalogue or the UI. Kept so existing configurations keep working; the
	// `rate` attribute carries a deprecation notice pointing at sample/percent.
	"sample_rate": {
		required:   []string{fieldRate},
		deprecated: true,
	},
}

// conditionFieldValue returns the value to serialize for one field, and whether
// the practitioner set it at all. Unset fields are omitted from the request
// rather than sent as an empty string or empty list.
func conditionFieldValue(
	cfg ResourcePipelineConditionConditionConfig,
	field string,
) (any, bool) {
	str := func(s types.String) (any, bool) {
		if s.IsNull() || s.IsUnknown() {
			return nil, false
		}
		return s.ValueString(), true
	}
	boolean := func(b types.Bool) (any, bool) {
		if b.IsNull() || b.IsUnknown() {
			return nil, false
		}
		return b.ValueBool(), true
	}

	switch field {
	case fieldKey:
		return str(cfg.Key)
	case fieldValue:
		return str(cfg.Value)
	case fieldPattern:
		return str(cfg.Pattern)
	case fieldRate:
		return str(cfg.Rate)
	case fieldPercent:
		if cfg.Percent.IsNull() || cfg.Percent.IsUnknown() {
			return nil, false
		}
		return cfg.Percent.ValueFloat64(), true
	case fieldValues:
		if cfg.Values.IsNull() || cfg.Values.IsUnknown() {
			return nil, false
		}
		elems := cfg.Values.Elements()
		out := make([]string, 0, len(elems))
		for _, e := range elems {
			if s, ok := e.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
				out = append(out, s.ValueString())
			}
		}
		return out, true
	case fieldNot:
		return boolean(cfg.Not)
	case fieldCaseInsensitive:
		return boolean(cfg.CaseInsensitive)
	case fieldRaw:
		return boolean(cfg.Raw)
	case fieldNull:
		return boolean(cfg.Null)
	case fieldWhitespaceString:
		return boolean(cfg.WhitespaceString)
	}
	return nil, false
}

// buildConditionConfig serializes one leaf's config, emitting only the fields
// its rule reads. An unknown type_id falls back to sending every field the
// practitioner set, so a rule added to the API after this provider was built
// still works.
func buildConditionConfig(
	typeID string,
	cfg ResourcePipelineConditionConditionConfig,
) map[string]any {
	out := map[string]any{}

	spec, known := conditionRules[typeID]
	fields := spec.fields()
	if !known {
		fields = []string{
			fieldKey, fieldValue, fieldValues, fieldPattern, fieldPercent, fieldRate,
			fieldNot, fieldCaseInsensitive, fieldRaw, fieldNull, fieldWhitespaceString,
		}
	}

	for _, f := range fields {
		if v, ok := conditionFieldValue(cfg, f); ok {
			out[f] = v
		}
	}
	return out
}

// conditionConfigFromAPI rebuilds the Terraform model from the config map the
// API returns, reading only the fields the rule owns so an unrelated key in the
// response never reads as drift.
func conditionConfigFromAPI(
	typeID string,
	cfg map[string]any,
) ResourcePipelineConditionConditionConfig {
	out := ResourcePipelineConditionConditionConfig{
		Key:              types.StringNull(),
		Value:            types.StringNull(),
		Values:           types.ListNull(types.StringType),
		Pattern:          types.StringNull(),
		Percent:          types.Float64Null(),
		Rate:             types.StringNull(),
		Not:              types.BoolNull(),
		CaseInsensitive:  types.BoolNull(),
		Raw:              types.BoolNull(),
		Null:             types.BoolNull(),
		WhitespaceString: types.BoolNull(),
	}
	if cfg == nil {
		return out
	}

	spec, known := conditionRules[typeID]
	allowed := func(f string) bool {
		if !known {
			return true
		}
		return spec.allows(f)
	}

	readString := func(f string) types.String {
		v, ok := cfg[f]
		if !ok || !allowed(f) {
			return types.StringNull()
		}
		if s := scalarToString(v); s != "" {
			return types.StringValue(s)
		}
		return types.StringNull()
	}
	readBool := func(f string) types.Bool {
		v, ok := cfg[f]
		if !ok || !allowed(f) {
			return types.BoolNull()
		}
		if b, ok := v.(bool); ok {
			return types.BoolValue(b)
		}
		return types.BoolNull()
	}

	out.Key = readString(fieldKey)
	out.Value = readString(fieldValue)
	out.Pattern = readString(fieldPattern)
	out.Rate = readString(fieldRate)
	out.Not = readBool(fieldNot)
	out.CaseInsensitive = readBool(fieldCaseInsensitive)
	out.Raw = readBool(fieldRaw)
	out.Null = readBool(fieldNull)
	out.WhitespaceString = readBool(fieldWhitespaceString)

	if v, ok := cfg[fieldPercent]; ok && allowed(fieldPercent) {
		switch n := v.(type) {
		case float64:
			out.Percent = types.Float64Value(n)
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				out.Percent = types.Float64Value(f)
			}
		}
	}

	if v, ok := cfg[fieldValues]; ok && allowed(fieldValues) {
		if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
			vals := make([]attr.Value, 0, len(arr))
			for _, e := range arr {
				vals = append(vals, types.StringValue(scalarToString(e)))
			}
			out.Values = types.ListValueMust(types.StringType, vals)
		}
	}

	return out
}

// scalarToString renders a JSON scalar the way the practitioner would have
// written it in HCL. The API stores `value` as given and its rules accept JSON
// syntax, so a number configured as "100" comes back as a float64 and must not
// render as "100.000000".
func scalarToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
		return ""
	}
}
