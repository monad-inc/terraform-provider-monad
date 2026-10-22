package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The threshold-alert settings schema as the API returned it on 2026-10-22,
// trimmed: a one_of with a discriminator, named_enum, and a plain field.
const thresholdSettingsJSON = `{
  "operator": {"name": "Operator", "type": "string", "required": true,
    "named_enum": [{"name": "Greater Than", "value": "greater_than"}, {"name": "Less Than", "value": "less_than"}]},
  "time_window": {"name": "Time Window", "type": "string", "required": true, "default": "5m"},
  "metric_config": {"name": "Metric Configuration", "type": "one_of", "required": true,
    "discriminator": {"name": "Metric Type", "field": "type", "one_of": {
      "records": {"name": "Records", "children": {
        "direction": {"name": "Direction", "type": "string", "required": true,
          "named_enum": [{"name": "Ingress", "value": "ingress"}, {"name": "Egress", "value": "egress"}]},
        "threshold": {"name": "Threshold", "type": "int", "required": true}}},
      "bytes": {"name": "Bytes", "children": {
        "unit": {"name": "Unit", "type": "string", "required": true, "enum": ["KB", "MB"]}}}}}}
}`

func TestFlattenTypeFieldsOneOf(t *testing.T) {
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(thresholdSettingsJSON), &meta))
	fields := flattenTypeFields(meta)

	byPath := map[string]typeField{}
	var paths []string
	for _, f := range fields {
		byPath[f.Path] = f
		paths = append(paths, f.Path)
	}
	assert.Equal(t, []string{
		"metric_config",
		"metric_config.bytes.unit",
		"metric_config.records.direction",
		"metric_config.records.threshold",
		"metric_config.type",
		"operator",
		"time_window",
	}, paths, "sorted, variants nested under their name")

	assert.Equal(t, []string{"bytes", "records"}, byPath["metric_config.type"].AllowedValues, "discriminator lists the variants")
	assert.Equal(t, []string{"greater_than", "less_than"}, byPath["operator"].AllowedValues, "named_enum values")
	assert.Equal(t, []string{"KB", "MB"}, byPath["metric_config.bytes.unit"].AllowedValues, "plain enum")
	require.NotNil(t, byPath["time_window"].Default)
	assert.Equal(t, `"5m"`, *byPath["time_window"].Default, "defaults are JSON-encoded")
	assert.Nil(t, byPath["operator"].Default)
	assert.True(t, byPath["metric_config.records.threshold"].Required)
	assert.Equal(t, "int", byPath["metric_config.records.threshold"].Type)
}

func TestFlattenTypeFieldsChildren(t *testing.T) {
	meta := map[string]any{
		"auth": map[string]any{"name": "Auth", "type": "object", "children": map[string]any{
			"client_id": map[string]any{"name": "Client ID", "type": "string", "required": true},
		}},
		"tags": map[string]any{"name": "Tags", "type": "array", "item_type": "string"},
	}
	fields := flattenTypeFields(meta)
	require.Len(t, fields, 3)
	assert.Equal(t, "auth.client_id", fields[1].Path)
	assert.Equal(t, "string", fields[2].ItemType)
}

func TestTypeFieldsValue(t *testing.T) {
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(thresholdSettingsJSON), &meta))
	list, err := typeFieldsValue(flattenTypeFields(meta))
	require.NoError(t, err)
	assert.Len(t, list.Elements(), 7)
}
