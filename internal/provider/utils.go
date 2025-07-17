package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ptr[T any](v T) *T {
	return &v
}

func getResponseBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body
}

// TfDynamicToMapAny converts a types.Dynamic to map[string]any
func TfDynamicToMapAny(dyn types.Dynamic) (map[string]any, error) {
	return tfDynamicToMapAny(dyn)
}

func tfDynamicToMapAny(dyn types.Dynamic) (map[string]any, error) {
	if dyn.IsNull() || dyn.IsUnknown() {
		return nil, nil
	}

	underlying := dyn.UnderlyingValue()
	switch value := underlying.(type) {
	case types.Object:
		return tfObjectToMapAny(context.Background(), value)
	case types.Map:
		return tfMapToMapAny(context.Background(), value)
	default:
		return nil, fmt.Errorf("dynamic value is not an object or map, got %T", underlying)
	}
}

func tfObjectToMapAny(ctx context.Context, obj types.Object) (map[string]any, error) {
	if obj.IsNull() || obj.IsUnknown() {
		return nil, nil
	}

	result := make(map[string]any)
	attrs := obj.Attributes()
	
	for key, attrValue := range attrs {
		converted, err := tfValueToAny(ctx, attrValue)
		if err != nil {
			return nil, fmt.Errorf("error converting attribute %q: %w", key, err)
		}
		result[key] = converted
	}
	
	return result, nil
}

func tfMapToMapAny(ctx context.Context, mapVal types.Map) (map[string]any, error) {
	if mapVal.IsNull() || mapVal.IsUnknown() {
		return nil, nil
	}

	result := make(map[string]any)
	elements := mapVal.Elements()
	
	for key, element := range elements {
		converted, err := tfValueToAny(ctx, element)
		if err != nil {
			return nil, fmt.Errorf("error converting map element %q: %w", key, err)
		}
		result[key] = converted
	}
	
	return result, nil
}

func tfValueToAny(ctx context.Context, value attr.Value) (any, error) {
	if value.IsNull() {
		return nil, nil
	}
	if value.IsUnknown() {
		return nil, fmt.Errorf("cannot convert unknown value to any")
	}

	switch v := value.(type) {
	case types.String:
		return v.ValueString(), nil
	case types.Bool:
		return v.ValueBool(), nil
	case types.Int64:
		return v.ValueInt64(), nil
	case types.Float64:
		return v.ValueFloat64(), nil
	case types.Number:
		val, _ := v.ValueBigFloat().Float64()
		return val, nil
	case types.List:
		return tfListToSliceAny(ctx, v)
	case types.Set:
		return tfSetToSliceAny(ctx, v)
	case types.Map:
		return tfMapToMapAny(ctx, v)
	case types.Object:
		return tfObjectToMapAny(ctx, v)
	case types.Dynamic:
		return tfDynamicToMapAny(v)
	default:
		return nil, fmt.Errorf("unsupported terraform type: %T", v)
	}
}

func tfListToSliceAny(ctx context.Context, list types.List) ([]any, error) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}

	elements := list.Elements()
	result := make([]any, len(elements))
	
	for i, element := range elements {
		converted, err := tfValueToAny(ctx, element)
		if err != nil {
			return nil, fmt.Errorf("error converting list element at index %d: %w", i, err)
		}
		result[i] = converted
	}
	
	return result, nil
}

func tfSetToSliceAny(ctx context.Context, set types.Set) ([]any, error) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}

	elements := set.Elements()
	result := make([]any, len(elements))
	
	i := 0
	for _, element := range elements {
		converted, err := tfValueToAny(ctx, element)
		if err != nil {
			return nil, fmt.Errorf("error converting set element: %w", err)
		}
		result[i] = converted
		i++
	}
	
	return result, nil
}

// anyToAttrValue converts a Go value to an attr.Value and attr.Type
func anyToAttrValue(v any) (attr.Value, attr.Type, error) {
	if v == nil {
		return types.StringNull(), types.StringType, nil
	}

	switch val := v.(type) {
	case string:
		return types.StringValue(val), types.StringType, nil
	case bool:
		return types.BoolValue(val), types.BoolType, nil
	case int:
		return types.Int64Value(int64(val)), types.Int64Type, nil
	case int32:
		return types.Int64Value(int64(val)), types.Int64Type, nil
	case int64:
		return types.Int64Value(val), types.Int64Type, nil
	case float32:
		return types.Float64Value(float64(val)), types.Float64Type, nil
	case float64:
		return types.Float64Value(val), types.Float64Type, nil
	case []any:
		// Convert slice to list
		elements := make([]attr.Value, len(val))
		var elementType attr.Type
		
		for i, elem := range val {
			elemValue, elemType, err := anyToAttrValue(elem)
			if err != nil {
				return nil, nil, fmt.Errorf("error converting slice element at index %d: %w", i, err)
			}
			elements[i] = elemValue
			if elementType == nil {
				elementType = elemType
			}
		}
		
		if elementType == nil {
			elementType = types.StringType // default type for empty slices
		}
		
		listValue, diags := types.ListValue(elementType, elements)
		if diags.HasError() {
			return nil, nil, fmt.Errorf("error creating list value: %s", diags)
		}
		return listValue, types.ListType{ElemType: elementType}, nil
		
	case map[string]any:
		// Convert map to object
		attributes := make(map[string]attr.Value)
		attributeTypes := make(map[string]attr.Type)
		
		for key, value := range val {
			attrValue, attrType, err := anyToAttrValue(value)
			if err != nil {
				return nil, nil, fmt.Errorf("error converting map value for key %q: %w", key, err)
			}
			attributes[key] = attrValue
			attributeTypes[key] = attrType
		}
		
		objectValue, diags := types.ObjectValue(attributeTypes, attributes)
		if diags.HasError() {
			return nil, nil, fmt.Errorf("error creating object value: %s", diags)
		}
		return objectValue, types.ObjectType{AttrTypes: attributeTypes}, nil
		
	default:
		// Handle interface{} values by using reflection
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.String:
			return types.StringValue(rv.String()), types.StringType, nil
		case reflect.Bool:
			return types.BoolValue(rv.Bool()), types.BoolType, nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return types.Int64Value(rv.Int()), types.Int64Type, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return types.Int64Value(int64(rv.Uint())), types.Int64Type, nil
		case reflect.Float32, reflect.Float64:
			return types.Float64Value(rv.Float()), types.Float64Type, nil
		default:
			return nil, nil, fmt.Errorf("unsupported Go type: %T (kind: %s)", v, rv.Kind())
		}
	}
}

// AnyToDynamic converts a map[string]any to types.Dynamic
func AnyToDynamic(in map[string]any) (types.Dynamic, error) {
	if in == nil || len(in) == 0 {
		return types.DynamicNull(), nil
	}
	
	// Convert the map to an ObjectValue
	attrValue, _, err := anyToAttrValue(in)
	if err != nil {
		return types.DynamicNull(), fmt.Errorf("error converting map to attr.Value: %w", err)
	}
	
	// Wrap the ObjectValue in a DynamicValue
	return types.DynamicValue(attrValue), nil
}
