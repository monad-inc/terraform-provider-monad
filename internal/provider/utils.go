package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func getResponseBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body
}

// hmacSHA256Hex computes an HMAC-SHA256 of value keyed by key, returned as a
// hex string. The key is zero-padded to the recommended 32-byte minimum.
func hmacSHA256Hex(ctx context.Context, key, value string) string {
	keyBytes := []byte(key)

	// Pad with zeros if the key is below the recommended 32 bytes.
	if len(keyBytes) < 32 {
		tflog.Warn(ctx, "HMAC key length is below recommended 32 bytes, padding with zeros", map[string]any{
			"original_length": len(keyBytes),
			"padded_length":   32,
		})
		padded := make([]byte, 32)
		copy(padded, keyBytes)
		keyBytes = padded
	}

	h := hmac.New(sha256.New, keyBytes)
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

// secretsHashKey returns the HMAC key used to fingerprint write-only secret
// values. It prefers MONAD_SECRETS_KEY, falling back to the organization ID.
// Keeping the key out of state means the stored hash cannot be brute-forced
// back to the secret without also knowing the key.
func secretsHashKey(orgID string) string {
	if k := os.Getenv("MONAD_SECRETS_KEY"); k != "" {
		return k
	}
	return orgID
}

// computeSecretsHash returns a stable HMAC fingerprint of a write-only secrets
// map, or "" when there are no secrets. json.Marshal sorts map keys, so the
// encoding — and therefore the hash — is deterministic for equal maps.
func computeSecretsHash(ctx context.Context, orgID string, secrets map[string]any) (string, error) {
	if len(secrets) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(secrets)
	if err != nil {
		return "", fmt.Errorf("failed to encode secrets for hashing: %w", err)
	}
	return hmacSHA256Hex(ctx, secretsHashKey(orgID), string(encoded)), nil
}

// dynamicsSemanticallyEqual reports whether two dynamic values carry the same
// data regardless of their concrete cty types. A practitioner's jsondecode
// yields tuples/objects while an API-derived value yields lists/maps; the two
// compare unequal by cty type even when the underlying data is identical.
// Normalizing both through a JSON round-trip collapses those representation
// differences (and unifies numeric types to float64) before comparison.
func dynamicsSemanticallyEqual(a, b map[string]any) bool {
	return reflect.DeepEqual(
		pruneEmpty(jsonNormalize(a)),
		pruneEmpty(jsonNormalize(b)),
	)
}

// pruneEmpty recursively removes "" / null / empty-object / empty-array map
// entries so that a field the practitioner set to the empty string compares
// equal to one the API omits (the API/SDK drops empty values via `omitempty`).
// Slice elements are pruned in place but never dropped, so array length and
// ordering — which are significant — are preserved. Booleans and numbers
// (including false/0) are left untouched, so a genuine value change is never
// masked.
func pruneEmpty(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			pv := pruneEmpty(val)
			if isEmptyForCompare(pv) {
				continue
			}
			out[k] = pv
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = pruneEmpty(e)
		}
		return out
	default:
		return v
	}
}

func isEmptyForCompare(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	default:
		return false
	}
}

// jsonNormalize collapses a value to its canonical JSON shape: map keys sorted,
// all numbers float64, and empty maps/slices treated as nil. It lets values
// that carry the same data but differ in concrete type compare equal.
func jsonNormalize(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case map[string]any:
		if len(t) == 0 {
			return nil
		}
	case []any:
		if len(t) == 0 {
			return nil
		}
	}

	encoded, err := json.Marshal(v)
	if err != nil {
		// Fall back to the raw value; a marshal failure here just means the
		// comparison is stricter, never wrong.
		return v
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return v
	}
	return out
}

// reconcileDynamic refreshes a Dynamic attribute for drift detection without
// churning its cty type. It keeps the prior state value (preserving the
// practitioner-authored representation) when the API-derived data is
// semantically equal, and only adopts the API value when real drift exists.
// On import the prior state is null, so the API value always populates.
func reconcileDynamic(prior types.Dynamic, apiValue map[string]any) (types.Dynamic, error) {
	priorMap, err := tfDynamicToMapAny(prior)
	if err != nil {
		// Prior state isn't a map/object we can normalize; adopt the API value.
		return AnyToDynamic(apiValue)
	}
	if dynamicsSemanticallyEqual(priorMap, apiValue) {
		return prior, nil
	}
	return AnyToDynamic(apiValue)
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
	case types.Tuple:
		return tfTupleToSliceAny(ctx, v)
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

func tfTupleToSliceAny(ctx context.Context, tuple types.Tuple) ([]any, error) {
	if tuple.IsNull() || tuple.IsUnknown() {
		return nil, nil
	}

	elements := tuple.Elements()
	result := make([]any, len(elements))

	for i, element := range elements {
		converted, err := tfValueToAny(ctx, element)
		if err != nil {
			return nil, fmt.Errorf("error converting tuple element at index %d: %w", i, err)
		}
		result[i] = converted
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
		// Convert slice to tuple (which can handle heterogeneous types)
		elements := make([]attr.Value, len(val))
		elementTypes := make([]attr.Type, len(val))

		for i, elem := range val {
			elemValue, elemType, err := anyToAttrValue(elem)
			if err != nil {
				return nil, nil, fmt.Errorf("error converting slice element at index %d: %w", i, err)
			}
			elements[i] = elemValue
			elementTypes[i] = elemType
		}

		if len(elements) == 0 {
			// For empty slices, return an empty tuple
			tupleValue, diags := types.TupleValue([]attr.Type{}, []attr.Value{})
			if diags.HasError() {
				return nil, nil, fmt.Errorf("error creating empty tuple value: %s", diags)
			}
			return tupleValue, types.TupleType{ElemTypes: []attr.Type{}}, nil
		}

		tupleValue, diags := types.TupleValue(elementTypes, elements)
		if diags.HasError() {
			return nil, nil, fmt.Errorf("error creating tuple value: %s", diags)
		}
		return tupleValue, types.TupleType{ElemTypes: elementTypes}, nil

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
	if len(in) == 0 {
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
