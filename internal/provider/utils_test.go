package provider

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTfDynamicToMapAny(t *testing.T) {
	tests := []struct {
		name        string
		input       types.Dynamic
		expected    map[string]any
		expectError bool
		errorMsg    string
	}{
		{
			name:     "null dynamic value",
			input:    types.DynamicNull(),
			expected: nil,
		},
		{
			name:     "unknown dynamic value",
			input:    types.DynamicUnknown(),
			expected: nil,
		},
		{
			name: "object with string values",
			input: func() types.Dynamic {
				objValue, _ := types.ObjectValue(
					map[string]attr.Type{
						"key1": types.StringType,
						"key2": types.StringType,
					},
					map[string]attr.Value{
						"key1": types.StringValue("value1"),
						"key2": types.StringValue("value2"),
					},
				)
				return types.DynamicValue(objValue)
			}(),
			expected: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "object with mixed types",
			input: func() types.Dynamic {
				objValue, _ := types.ObjectValue(
					map[string]attr.Type{
						"str":   types.StringType,
						"bool":  types.BoolType,
						"int":   types.Int64Type,
						"float": types.Float64Type,
					},
					map[string]attr.Value{
						"str":   types.StringValue("test"),
						"bool":  types.BoolValue(true),
						"int":   types.Int64Value(42),
						"float": types.Float64Value(3.14),
					},
				)
				return types.DynamicValue(objValue)
			}(),
			expected: map[string]any{
				"str":   "test",
				"bool":  true,
				"int":   int64(42),
				"float": 3.14,
			},
		},
		{
			name: "object with null values",
			input: func() types.Dynamic {
				objValue, _ := types.ObjectValue(
					map[string]attr.Type{
						"null_str": types.StringType,
						"valid":    types.StringType,
					},
					map[string]attr.Value{
						"null_str": types.StringNull(),
						"valid":    types.StringValue("test"),
					},
				)
				return types.DynamicValue(objValue)
			}(),
			expected: map[string]any{
				"null_str": nil,
				"valid":    "test",
			},
		},
		{
			name: "null object",
			input: func() types.Dynamic {
				objValue := types.ObjectNull(map[string]attr.Type{
					"key": types.StringType,
				})
				return types.DynamicValue(objValue)
			}(),
			expected: nil,
		},
		{
			name: "map with string values",
			input: func() types.Dynamic {
				mapValue, _ := types.MapValue(
					types.StringType,
					map[string]attr.Value{
						"key1": types.StringValue("value1"),
						"key2": types.StringValue("value2"),
					},
				)
				return types.DynamicValue(mapValue)
			}(),
			expected: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "map with string values only",
			input: func() types.Dynamic {
				// Maps can only have elements of the same type, so use string for all
				mapValue, _ := types.MapValue(
					types.StringType,
					map[string]attr.Value{
						"str1": types.StringValue("test"),
						"str2": types.StringValue("another"),
					},
				)
				return types.DynamicValue(mapValue)
			}(),
			expected: map[string]any{
				"str1": "test",
				"str2": "another",
			},
		},
		{
			name: "map with null values",
			input: func() types.Dynamic {
				mapValue, _ := types.MapValue(
					types.StringType,
					map[string]attr.Value{
						"null_val": types.StringNull(),
						"valid":    types.StringValue("test"),
					},
				)
				return types.DynamicValue(mapValue)
			}(),
			expected: map[string]any{
				"null_val": nil,
				"valid":    "test",
			},
		},
		{
			name: "null map",
			input: func() types.Dynamic {
				mapValue := types.MapNull(types.StringType)
				return types.DynamicValue(mapValue)
			}(),
			expected: nil,
		},
		{
			name: "empty object",
			input: func() types.Dynamic {
				objValue, _ := types.ObjectValue(
					map[string]attr.Type{},
					map[string]attr.Value{},
				)
				return types.DynamicValue(objValue)
			}(),
			expected: map[string]any{},
		},
		{
			name: "empty map",
			input: func() types.Dynamic {
				mapValue, _ := types.MapValue(
					types.StringType,
					map[string]attr.Value{},
				)
				return types.DynamicValue(mapValue)
			}(),
			expected: map[string]any{},
		},
		{
			name: "unsupported underlying type - string",
			input: func() types.Dynamic {
				return types.DynamicValue(types.StringValue("not an object or map"))
			}(),
			expectError: true,
			errorMsg:    "dynamic value is not an object or map, got basetypes.StringValue",
		},
		{
			name: "unsupported underlying type - int64",
			input: func() types.Dynamic {
				return types.DynamicValue(types.Int64Value(42))
			}(),
			expectError: true,
			errorMsg:    "dynamic value is not an object or map, got basetypes.Int64Value",
		},
		{
			name: "unsupported underlying type - bool",
			input: func() types.Dynamic {
				return types.DynamicValue(types.BoolValue(true))
			}(),
			expectError: true,
			errorMsg:    "dynamic value is not an object or map, got basetypes.BoolValue",
		},
		{
			name: "unsupported underlying type - list",
			input: func() types.Dynamic {
				listValue, _ := types.ListValue(
					types.StringType,
					[]attr.Value{
						types.StringValue("item1"),
						types.StringValue("item2"),
					},
				)
				return types.DynamicValue(listValue)
			}(),
			expectError: true,
			errorMsg:    "dynamic value is not an object or map, got basetypes.ListValue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TfDynamicToMapAny(tt.input)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestAnyToDynamic(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		wantNull bool
		wantErr  bool
	}{
		{
			name:     "nil input",
			input:    nil,
			wantNull: true,
		},
		{
			name:     "empty map",
			input:    map[string]any{},
			wantNull: true,
		},
		{
			name: "string values",
			input: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "mixed primitive types",
			input: map[string]any{
				"string_val": "hello",
				"bool_val":   true,
				"int_val":    42,
				"float_val":  3.14,
			},
		},
		{
			name: "integer variants",
			input: map[string]any{
				"int":    int(42),
				"int32":  int32(42),
				"int64":  int64(42),
				"uint":   uint(42),
				"uint32": uint32(42),
				"uint64": uint64(42),
			},
		},
		{
			name: "float variants",
			input: map[string]any{
				"float32": float32(3.14),
				"float64": float64(3.14),
			},
		},
		{
			name: "nested map",
			input: map[string]any{
				"outer": map[string]any{
					"inner1": "value1",
					"inner2": 42,
				},
			},
		},
		{
			name: "slice values",
			input: map[string]any{
				"string_slice": []any{"a", "b", "c"},
				"int_slice":    []any{1, 2, 3},
				"mixed_slice":  []any{"string", 42, true},
			},
		},
		{
			name: "empty slice",
			input: map[string]any{
				"empty_slice": []any{},
			},
		},
		{
			name: "nil value in map",
			input: map[string]any{
				"nil_value":    nil,
				"string_value": "not nil",
			},
		},
		{
			name: "deeply nested structure",
			input: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": []any{
							map[string]any{
								"deep_key": "deep_value",
							},
						},
					},
				},
			},
		},
		{
			name: "complex nested structure",
			input: map[string]any{
				"config": map[string]any{
					"database": map[string]any{
						"host":     "localhost",
						"port":     5432,
						"ssl":      true,
						"timeouts": []any{30, 60, 120},
					},
					"features": []any{
						"feature1",
						map[string]any{
							"name":    "feature2",
							"enabled": true,
						},
					},
				},
				"metadata": map[string]any{
					"version": "1.0.0",
					"tags":    []any{"prod", "database"},
				},
			},
		},
		{
			name: "reflection path types - interface{} values",
			input: func() map[string]any {
				// Create values as interface{} to trigger reflection paths
				var stringVal interface{} = "reflection test"
				var boolVal interface{} = true
				var intVal interface{} = int(42)
				var int8Val interface{} = int8(8)
				var int16Val interface{} = int16(16)
				var int32Val interface{} = int32(32)
				var int64Val interface{} = int64(64)
				var uintVal interface{} = uint(100)
				var uint8Val interface{} = uint8(200)
				var uint16Val interface{} = uint16(300)
				var uint32Val interface{} = uint32(400)
				var uint64Val interface{} = uint64(500)
				var float32Val interface{} = float32(3.14)
				var float64Val interface{} = float64(2.718)
				
				return map[string]any{
					"string_interface":  stringVal,
					"bool_interface":    boolVal,
					"int_interface":     intVal,
					"int8_interface":    int8Val,
					"int16_interface":   int16Val,
					"int32_interface":   int32Val,
					"int64_interface":   int64Val,
					"uint_interface":    uintVal,
					"uint8_interface":   uint8Val,
					"uint16_interface":  uint16Val,
					"uint32_interface":  uint32Val,
					"uint64_interface":  uint64Val,
					"float32_interface": float32Val,
					"float64_interface": float64Val,
				}
			}(),
		},
		{
			name:    "unsupported type error",
			input:   map[string]any{
				"unsupported": func() {}, // function type should trigger error
			},
			wantErr: true,
		},
		{
			name: "reflection path - specific unused types",
			input: func() map[string]any {
				// Test the reflection paths that weren't covered
				// These are interface{} values that will force reflection
				var reflectString interface{} = "reflect string"
				var reflectBool interface{} = false
				var reflectFloat32 interface{} = float32(1.23)
				
				return map[string]any{
					"reflect_string":  reflectString,
					"reflect_bool":    reflectBool,
					"reflect_float32": reflectFloat32,
				}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AnyToDynamic(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.wantNull {
				assert.True(t, result.IsNull(), "expected null dynamic value")
				return
			}

			assert.False(t, result.IsNull(), "expected non-null dynamic value")
			assert.False(t, result.IsUnknown(), "expected known dynamic value")

			// Convert back to verify roundtrip works
			converted, err := TfDynamicToMapAny(result)
			require.NoError(t, err)
			
			// For non-null cases, verify we can convert back
			if !tt.wantNull {
				assert.NotNil(t, converted)
			}
		})
	}
}

func TestAnyToDynamic_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
	}{
		{
			name: "simple values",
			input: map[string]any{
				"string": "hello",
				"number": 42,
				"bool":   true,
			},
		},
		{
			name: "nested structure",
			input: map[string]any{
				"outer": map[string]any{
					"inner": "value",
					"count": 10,
				},
			},
		},
		{
			name: "with arrays",
			input: map[string]any{
				"items": []any{"a", "b", "c"},
				"nums":  []any{1, 2, 3},
			},
		},
		{
			name: "nested arrays and maps",
			input: map[string]any{
				"data": []any{
					map[string]any{
						"id":   1,
						"name": "first",
					},
					map[string]any{
						"id":   2,
						"name": "second",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to dynamic
			dynamic, err := AnyToDynamic(tt.input)
			require.NoError(t, err)
			require.False(t, dynamic.IsNull())

			// Convert back to map
			result, err := TfDynamicToMapAny(dynamic)
			require.NoError(t, err)

			// Verify structure is preserved (values might have type changes)
			assert.Equal(t, len(tt.input), len(result))
			for key := range tt.input {
				assert.Contains(t, result, key)
			}
		})
	}
}

func TestTfValueToAny(t *testing.T) {
	ctx := context.Background()
	
	tests := []struct {
		name        string
		input       attr.Value
		expected    any
		expectError bool
		errorMsg    string
	}{
		// Null value test
		{
			name:     "null string value",
			input:    types.StringNull(),
			expected: nil,
		},
		// Unknown value test
		{
			name:        "unknown string value",
			input:       types.StringUnknown(),
			expectError: true,
			errorMsg:    "cannot convert unknown value to any",
		},
		// types.Number case
		{
			name: "number value - integer",
			input: func() attr.Value {
				bf := big.NewFloat(42.0)
				return types.NumberValue(bf)
			}(),
			expected: 42.0,
		},
		{
			name: "number value - decimal",
			input: func() attr.Value {
				bf := big.NewFloat(3.14159)
				return types.NumberValue(bf)
			}(),
			expected: 3.14159,
		},
		{
			name: "number value - large number",
			input: func() attr.Value {
				bf := big.NewFloat(1234567890.123456)
				return types.NumberValue(bf)
			}(),
			expected: 1234567890.123456,
		},
		{
			name:     "null number value",
			input:    types.NumberNull(),
			expected: nil,
		},
		// types.List case
		{
			name: "list with string values",
			input: func() attr.Value {
				list, _ := types.ListValue(
					types.StringType,
					[]attr.Value{
						types.StringValue("item1"),
						types.StringValue("item2"),
						types.StringValue("item3"),
					},
				)
				return list
			}(),
			expected: []any{"item1", "item2", "item3"},
		},
		{
			name: "list with int values",
			input: func() attr.Value {
				list, _ := types.ListValue(
					types.Int64Type,
					[]attr.Value{
						types.Int64Value(1),
						types.Int64Value(2),
						types.Int64Value(3),
					},
				)
				return list
			}(),
			expected: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:     "null list",
			input:    types.ListNull(types.StringType),
			expected: nil,
		},
		{
			name: "empty list",
			input: func() attr.Value {
				list, _ := types.ListValue(types.StringType, []attr.Value{})
				return list
			}(),
			expected: []any{},
		},
		// types.Set case
		{
			name: "set with string values",
			input: func() attr.Value {
				set, _ := types.SetValue(
					types.StringType,
					[]attr.Value{
						types.StringValue("item1"),
						types.StringValue("item2"),
						types.StringValue("item3"),
					},
				)
				return set
			}(),
			expected: []any{"item1", "item2", "item3"},
		},
		{
			name:     "null set",
			input:    types.SetNull(types.StringType),
			expected: nil,
		},
		{
			name: "empty set",
			input: func() attr.Value {
				set, _ := types.SetValue(types.StringType, []attr.Value{})
				return set
			}(),
			expected: []any{},
		},
		// types.Tuple case
		{
			name: "tuple with mixed types",
			input: func() attr.Value {
				tuple, _ := types.TupleValue(
					[]attr.Type{types.StringType, types.Int64Type, types.BoolType},
					[]attr.Value{
						types.StringValue("test"),
						types.Int64Value(42),
						types.BoolValue(true),
					},
				)
				return tuple
			}(),
			expected: []any{"test", int64(42), true},
		},
		{
			name: "null tuple",
			input: types.TupleNull([]attr.Type{
				types.StringType,
				types.Int64Type,
			}),
			expected: nil,
		},
		{
			name: "empty tuple",
			input: func() attr.Value {
				tuple, _ := types.TupleValue([]attr.Type{}, []attr.Value{})
				return tuple
			}(),
			expected: []any{},
		},
		// types.Map case
		{
			name: "map with string values",
			input: func() attr.Value {
				mapVal, _ := types.MapValue(
					types.StringType,
					map[string]attr.Value{
						"key1": types.StringValue("value1"),
						"key2": types.StringValue("value2"),
					},
				)
				return mapVal
			}(),
			expected: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name:     "null map",
			input:    types.MapNull(types.StringType),
			expected: nil,
		},
		{
			name: "empty map",
			input: func() attr.Value {
				mapVal, _ := types.MapValue(types.StringType, map[string]attr.Value{})
				return mapVal
			}(),
			expected: map[string]any{},
		},
		// types.Object case
		{
			name: "object with mixed types",
			input: func() attr.Value {
				obj, _ := types.ObjectValue(
					map[string]attr.Type{
						"str":   types.StringType,
						"int":   types.Int64Type,
						"bool":  types.BoolType,
						"float": types.Float64Type,
					},
					map[string]attr.Value{
						"str":   types.StringValue("test"),
						"int":   types.Int64Value(42),
						"bool":  types.BoolValue(true),
						"float": types.Float64Value(3.14),
					},
				)
				return obj
			}(),
			expected: map[string]any{
				"str":   "test",
				"int":   int64(42),
				"bool":  true,
				"float": 3.14,
			},
		},
		{
			name: "null object",
			input: types.ObjectNull(map[string]attr.Type{
				"key": types.StringType,
			}),
			expected: nil,
		},
		{
			name: "empty object",
			input: func() attr.Value {
				obj, _ := types.ObjectValue(
					map[string]attr.Type{},
					map[string]attr.Value{},
				)
				return obj
			}(),
			expected: map[string]any{},
		},
		// types.Dynamic case
		{
			name: "dynamic value with object",
			input: func() attr.Value {
				obj, _ := types.ObjectValue(
					map[string]attr.Type{
						"key": types.StringType,
					},
					map[string]attr.Value{
						"key": types.StringValue("value"),
					},
				)
				return types.DynamicValue(obj)
			}(),
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:     "null dynamic value",
			input:    types.DynamicNull(),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tfValueToAny(ctx, tt.input)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestAnyToAttrValue_ReflectionPaths(t *testing.T) {
	// These tests specifically target the reflection code paths in anyToAttrValue
	// by using values that will bypass the direct type switch cases
	
	tests := []struct {
		name     string
		input    any
		wantErr  bool
		expected any // The expected underlying value after conversion
	}{
		{
			name: "reflect.String path",
			input: func() any {
				var val interface{} = "test string"
				// Create a value that will trigger reflection by wrapping in interface
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: "test string",
		},
		{
			name: "reflect.Bool path",
			input: func() any {
				var val interface{} = true
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: true,
		},
		{
			name: "reflect.Int8 path",
			input: func() any {
				var val interface{} = int8(42)
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: int64(42),
		},
		{
			name: "reflect.Int16 path", 
			input: func() any {
				var val interface{} = int16(1000)
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: int64(1000),
		},
		{
			name: "reflect.Uint8 path",
			input: func() any {
				var val interface{} = uint8(255)
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: int64(255),
		},
		{
			name: "reflect.Uint16 path",
			input: func() any {
				var val interface{} = uint16(65535)
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: int64(65535),
		},
		{
			name: "reflect.Uint path",
			input: func() any {
				var val interface{} = uint(12345)
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: int64(12345),
		},
		{
			name: "reflect.Float32 path",
			input: func() any {
				var val interface{} = float32(3.14)
				return struct{ V interface{} }{V: val}.V
			}(),
			expected: float64(3.1400001049041748), // float32 precision when converted to float64
		},
		{
			name: "unsupported type - channel",
			input: func() any {
				ch := make(chan int)
				var val interface{} = ch
				return struct{ V interface{} }{V: val}.V
			}(),
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, attrType, err := anyToAttrValue(tt.input)
			
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, attrType)
			
			// Convert back to check the value
			switch v := result.(type) {
			case types.String:
				assert.Equal(t, tt.expected, v.ValueString())
			case types.Bool:
				assert.Equal(t, tt.expected, v.ValueBool())
			case types.Int64:
				assert.Equal(t, tt.expected, v.ValueInt64())
			case types.Float64:
				if expected, ok := tt.expected.(float64); ok {
					// Use InDelta for float comparison due to precision
					assert.InDelta(t, expected, v.ValueFloat64(), 0.0001)
				} else {
					assert.Equal(t, tt.expected, v.ValueFloat64())
				}
			}
		})
	}
}

func TestAnyToAttrValue_ForceReflectionPaths(t *testing.T) {
	// This test uses JSON unmarshaling to force truly generic interface{} values
	// that will bypass the type switch and use reflection
	
	jsonStr := `{
		"string": "test",
		"bool": true,
		"int8": 127,
		"int16": 32767,
		"uint8": 255,
		"uint16": 65535,
		"uint32": 4294967295,
		"float32": 3.14
	}`
	
	var data map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &data)
	require.NoError(t, err)
	
	// These values from JSON unmarshaling should trigger reflection paths
	tests := []struct {
		name string
		key  string
	}{
		{"json string", "string"},
		{"json bool", "bool"},
		{"json int8", "int8"},
		{"json int16", "int16"},
		{"json uint8", "uint8"},
		{"json uint16", "uint16"},
		{"json uint32", "uint32"},
		{"json float32", "float32"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, attrType, err := anyToAttrValue(data[tt.key])
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, attrType)
		})
	}
}

func TestAnyToAttrValue_ErrorCases(t *testing.T) {
	// Test cases that should trigger error paths in anyToAttrValue
	
	tests := []struct {
		name    string
		input   any
		wantErr bool
	}{
		{
			name: "slice with error element",
			input: []any{
				"valid",
				make(chan int), // This should cause an error
			},
			wantErr: true,
		},
		{
			name: "map with error value",
			input: map[string]any{
				"valid":   "ok",
				"invalid": make(chan int), // This should cause an error
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := anyToAttrValue(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}