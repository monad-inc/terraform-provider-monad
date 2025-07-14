package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"

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
