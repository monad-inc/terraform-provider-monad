package provider

import (
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nullConditionConfig() ResourcePipelineConditionConditionConfig {
	return ResourcePipelineConditionConditionConfig{
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
}

func stringList(vals ...string) types.List {
	elems := make([]attr.Value, 0, len(vals))
	for _, v := range vals {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The bug in ENG-9546: every rule got {key, value, rate} regardless of type_id,
// so `value` was always an array and the API's comparison never matched. Each
// rule must now emit exactly the keys it reads.
func TestBuildConditionConfigEmitsOnlyTheRulesFields(t *testing.T) {
	cases := []struct {
		name     string
		typeID   string
		config   func(*ResourcePipelineConditionConditionConfig)
		wantKeys []string
		wantVals map[string]any
	}{
		{
			name:   "equals sends a scalar value, not a list",
			typeID: "equals",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("tier")
				c.Value = types.StringValue("hot")
			},
			wantKeys: []string{"key", "value"},
			wantVals: map[string]any{"key": "tier", "value": "hot"},
		},
		{
			name:   "equals_any sends values, not value",
			typeID: "equals_any",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("tier")
				c.Values = stringList("hot", "warm")
			},
			wantKeys: []string{"key", "values"},
			wantVals: map[string]any{"key": "tier", "values": []string{"hot", "warm"}},
		},
		{
			name:   "matches_regex sends pattern",
			typeID: "matches_regex",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("name")
				c.Pattern = types.StringValue("^prod-")
			},
			wantKeys: []string{"key", "pattern"},
			wantVals: map[string]any{"pattern": "^prod-"},
		},
		{
			name:   "sample sends percent and omits key when unset",
			typeID: "sample",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Percent = types.Float64Value(12.5)
			},
			wantKeys: []string{"percent"},
			wantVals: map[string]any{"percent": 12.5},
		},
		{
			name:   "greater_than sends the numeric value as a string",
			typeID: "greater_than",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("count")
				c.Value = types.StringValue("100")
			},
			wantKeys: []string{"key", "value"},
			wantVals: map[string]any{"value": "100"},
		},
		{
			name:   "key_exists sends only key, no empty value or rate",
			typeID: "key_exists",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("monad.route.hot")
			},
			wantKeys: []string{"key"},
		},
		{
			name:   "is_empty carries its own flags",
			typeID: "is_empty",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("payload")
				c.Null = types.BoolValue(true)
				c.WhitespaceString = types.BoolValue(true)
			},
			wantKeys: []string{"key", "null", "whitespace_string"},
			wantVals: map[string]any{"null": true, "whitespace_string": true},
		},
		{
			name:   "contains carries raw and case_insensitive",
			typeID: "contains",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("msg")
				c.Value = types.StringValue("denied")
				c.Raw = types.BoolValue(true)
				c.CaseInsensitive = types.BoolValue(true)
			},
			wantKeys: []string{"case_insensitive", "key", "raw", "value"},
		},
		{
			name:   "not is carried through",
			typeID: "equals",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("tier")
				c.Value = types.StringValue("cold")
				c.Not = types.BoolValue(true)
			},
			wantKeys: []string{"key", "not", "value"},
			wantVals: map[string]any{"not": true},
		},
		{
			name:   "legacy sample_rate still sends rate",
			typeID: "sample_rate",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Rate = types.StringValue("1s")
			},
			wantKeys: []string{"rate"},
			wantVals: map[string]any{"rate": "1s"},
		},
		{
			name:   "a field the rule ignores is not sent",
			typeID: "key_exists",
			config: func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("k")
				c.Percent = types.Float64Value(50)
				c.Pattern = types.StringValue("nope")
			},
			wantKeys: []string{"key", "not"}[:1],
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := nullConditionConfig()
			tc.config(&cfg)

			got := buildConditionConfig(tc.typeID, cfg)
			assert.Equal(t, tc.wantKeys, keysOf(got))
			for k, want := range tc.wantVals {
				assert.Equal(t, want, got[k], "field %q", k)
			}
		})
	}
}

// An unknown type_id must not silently drop the practitioner's fields — a rule
// the API ships after this provider was built should still work.
func TestBuildConditionConfigUnknownRuleSendsEverythingSet(t *testing.T) {
	cfg := nullConditionConfig()
	cfg.Key = types.StringValue("k")
	cfg.Value = types.StringValue("v")

	got := buildConditionConfig("some_future_rule", cfg)
	assert.Equal(t, []string{"key", "value"}, keysOf(got))
}

func TestConditionConfigRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		typeID string
		build  func(*ResourcePipelineConditionConditionConfig)
	}{
		{"equals", "equals", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("tier")
			c.Value = types.StringValue("hot")
			c.CaseInsensitive = types.BoolValue(true)
		}},
		{"equals_any", "equals_any", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("tier")
			c.Values = stringList("hot", "warm")
		}},
		{"sample", "sample", func(c *ResourcePipelineConditionConditionConfig) {
			c.Percent = types.Float64Value(12.5)
			c.Key = types.StringValue("user.id")
		}},
		{"matches_regex", "matches_regex", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("name")
			c.Pattern = types.StringValue("^prod-")
			c.Not = types.BoolValue(true)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := nullConditionConfig()
			tc.build(&original)

			wire := buildConditionConfig(tc.typeID, original)
			// Emulate the JSON round-trip through the API: []string and float64
			// come back as []interface{} and float64.
			decoded := map[string]any{}
			for k, v := range wire {
				if ss, ok := v.([]string); ok {
					arr := make([]interface{}, 0, len(ss))
					for _, s := range ss {
						arr = append(arr, s)
					}
					decoded[k] = arr
					continue
				}
				decoded[k] = v
			}

			assert.Equal(t, original, conditionConfigFromAPI(tc.typeID, decoded))
		})
	}
}

// The API stores `value` as given and its rules accept JSON syntax, so a
// numeric value can come back as a float64. It must not render as 100.000000.
func TestConditionConfigFromAPINumericValueFormatting(t *testing.T) {
	got := conditionConfigFromAPI("greater_than", map[string]any{
		"key":   "count",
		"value": float64(100),
	})
	assert.Equal(t, "100", got.Value.ValueString())

	got = conditionConfigFromAPI("greater_than", map[string]any{
		"key":   "ratio",
		"value": 12.5,
	})
	assert.Equal(t, "12.5", got.Value.ValueString())
}

// A key the rule does not own must not land in state, or it reads as drift.
func TestConditionConfigFromAPIIgnoresForeignFields(t *testing.T) {
	got := conditionConfigFromAPI("key_exists", map[string]any{
		"key":     "monad.route.hot",
		"value":   "leftover",
		"percent": 50.0,
	})
	assert.Equal(t, "monad.route.hot", got.Key.ValueString())
	assert.True(t, got.Value.IsNull(), "value belongs to other rules")
	assert.True(t, got.Percent.IsNull(), "percent belongs to sample")
}

func validateLeaf(t *testing.T, typeID string, build func(*ResourcePipelineConditionConditionConfig)) *resource.ValidateConfigResponse {
	t.Helper()
	cfg := nullConditionConfig()
	if build != nil {
		build(&cfg)
	}
	leaf := ResourcePipelineConditionCondition{TypeID: types.StringNull(), Config: cfg}
	if typeID != "" {
		leaf.TypeID = types.StringValue(typeID)
	}

	resp := &resource.ValidateConfigResponse{}
	validateConditionLeaf(leaf, path.Root("edges").AtListIndex(0).AtName("config"), resp)
	return resp
}

func TestValidateConditionLeaf(t *testing.T) {
	t.Run("equals_any without values errors", func(t *testing.T) {
		resp := validateLeaf(t, "equals_any", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("tier")
		})
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "requires \"values\"")
	})

	t.Run("equals_any with value instead of values errors", func(t *testing.T) {
		resp := validateLeaf(t, "equals_any", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("tier")
			c.Values = stringList("hot")
			c.Value = types.StringValue("hot")
		})
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "does not read \"value\"")
	})

	t.Run("sample without percent errors", func(t *testing.T) {
		resp := validateLeaf(t, "sample", nil)
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "requires \"percent\"")
	})

	t.Run("matches_regex without pattern errors", func(t *testing.T) {
		resp := validateLeaf(t, "matches_regex", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("name")
		})
		require.True(t, resp.Diagnostics.HasError())
	})

	t.Run("missing type_id errors", func(t *testing.T) {
		resp := validateLeaf(t, "", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("k")
		})
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing condition type_id")
	})

	t.Run("unknown type_id warns but does not error", func(t *testing.T) {
		resp := validateLeaf(t, "some_future_rule", func(c *ResourcePipelineConditionConditionConfig) {
			c.Key = types.StringValue("k")
		})
		assert.False(t, resp.Diagnostics.HasError())
		assert.Equal(t, 1, resp.Diagnostics.WarningsCount())
	})

	t.Run("valid leaves pass", func(t *testing.T) {
		for _, tc := range []struct {
			typeID string
			build  func(*ResourcePipelineConditionConditionConfig)
		}{
			{"equals", func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("tier")
				c.Value = types.StringValue("hot")
			}},
			{"equals_any", func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("tier")
				c.Values = stringList("hot", "warm")
			}},
			{"key_exists", func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("monad.route.hot")
			}},
			{"sample", func(c *ResourcePipelineConditionConditionConfig) {
				c.Percent = types.Float64Value(50)
			}},
			{"is_empty", func(c *ResourcePipelineConditionConditionConfig) {
				c.Key = types.StringValue("payload")
				c.Null = types.BoolValue(true)
			}},
		} {
			resp := validateLeaf(t, tc.typeID, tc.build)
			assert.False(t, resp.Diagnostics.HasError(), "type_id %q should validate: %v", tc.typeID, resp.Diagnostics)
		}
	})
}

// Every rule in the table must declare at least one required field, or
// validation silently accepts an empty leaf.
func TestConditionRulesTableIsWellFormed(t *testing.T) {
	for name, spec := range conditionRules {
		assert.NotEmpty(t, spec.required, "rule %q declares no required fields", name)
		for _, f := range append(append([]string{}, spec.required...), spec.optional...) {
			assert.Contains(t, allConditionFields, f, "rule %q references unknown field %q", name, f)
		}
	}
}
