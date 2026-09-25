package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

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

// isNotFoundResponse reports whether an errored API response indicates the
// resource no longer exists remotely. It is consulted only on a read error
// path, so any status seen here already represents a failed lookup.
//
// The Monad API currently returns HTTP 500 with the body
// {"code":500,"error":"An item of this type does not exist."} for a missing
// resource instead of a 404 (ENG-9258). We treat a 404/410 *or* that specific
// sentinel as not-found, so a resource deleted outside Terraform is dropped
// from state and recreated on the next plan rather than wedging plan/apply on
// refresh (ENG-9259). Once ENG-9258 ships the 404, the sentinel branch becomes
// redundant and can be removed.
// isTimeoutError reports whether err is the client giving up on a request --
// the HTTP client's per-request timeout, a cancelled/expired context, or any
// net.Error that reports Timeout(). The SDK returns transport failures
// unwrapped (a *url.Error), so errors.As finds them directly.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// net/http reports a Client.Timeout as a *url.Error whose Timeout() is true
	// (handled above) but older paths surface only the message; match it too.
	return strings.Contains(err.Error(), "Client.Timeout exceeded")
}

// withOperationTimeout derives the context an API operation runs under from
// the resource's `timeouts { … }` block (get is one of timeouts.Value.Create /
// Read / Update / Delete), falling back to the provider-level request_timeout.
// The returned cancel must be deferred by the caller.
func withOperationTimeout(
	ctx context.Context,
	get func(context.Context, time.Duration) (time.Duration, diag.Diagnostics),
	fallback time.Duration,
	diags *diag.Diagnostics,
) (context.Context, context.CancelFunc) {
	d, ds := get(ctx, fallback)
	diags.Append(ds...)
	if diags.HasError() || d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// adoptTimeoutGrace is how long adoptAfterTimeout keeps polling for an object
// whose create was still in flight when the client gave up, and
// adoptPollInterval how often. Variables so tests can shorten them.
var (
	adoptTimeoutGrace = 2 * time.Minute
	adoptPollInterval = 5 * time.Second
)

// adoptCandidate is the part of a listed object adoptAfterTimeout needs. Every
// Monad list endpoint returns id, name and created_at; connectors add type.
type adoptCandidate struct {
	ID        *string
	Name      *string
	Type      *string
	CreatedAt *string
}

// adoptLister fetches one page of the organization's objects of one resource
// type, plus the total the API reports (nil when it doesn't say). Each
// resource supplies its own, since each list call is a differently-typed SDK
// API.
type adoptLister func(ctx context.Context, limit, offset int32) ([]adoptCandidate, *int32, *http.Response, error)

// adoptTarget describes the object a timed-out create was trying to make.
type adoptTarget struct {
	// Kind names the resource in messages ("pipeline", "alert rule", …).
	Kind string
	Name string
	// Type, when set, must equal a candidate's type (connectors of different
	// types may share a name).
	Type string
	// StartedAt is when the create request was sent; candidates created more
	// than a minute before it (clock-skew allowance) are not this create's.
	StartedAt time.Time
	// AnyAge disables the created_at filter, for creates that upsert by name
	// (monad_secret): the object this create touched may predate it.
	AnyAge bool
	List   adoptLister
}

// createOrAdopt resolves the id a create call produced. On success that is
// createdID (the response's id). On failure it is either an adopted id or an
// error diagnostic: a timeout is ambiguous — the API often finishes a create
// after the client gives up — so rather than report failure and leave the
// object on the server but out of state (the next apply would create a
// duplicate, ENG-10257 / ENG-10511), it looks the object up by name and adopts
// it when that is unambiguous. Any other error is reported as a client error.
//
// ctx must outlive the create's own timeout (pass the context from before
// withOperationTimeout); the provider-level request_timeout still bounds each
// list request. ok is false when an error diagnostic was added.
func createOrAdopt(
	ctx context.Context,
	diags *diag.Diagnostics,
	t adoptTarget,
	createdID string,
	err error,
	httpResp *http.Response,
) (id string, ok bool) {
	if err == nil {
		return createdID, true
	}
	if !isTimeoutError(err) {
		diags.AddError(
			"Client Error",
			fmt.Sprintf("Unable to create %s, got error: %s. Response: %s", t.Kind, err, getResponseBody(httpResp)),
		)
		return "", false
	}
	kind := strings.ToUpper(t.Kind[:1]) + t.Kind[1:]
	id, adoptErr := adoptAfterTimeout(ctx, t)
	if adoptErr != nil {
		diags.AddError(
			kind+" create timed out",
			fmt.Sprintf("The request to create %s %q exceeded its create timeout: %s. %s", t.Kind, t.Name, err, adoptErr),
		)
		return "", false
	}
	diags.AddWarning(
		kind+" create timed out but completed on the server",
		fmt.Sprintf(
			"The request to create %s %q exceeded its create timeout, "+
				"but the API finished creating it as %s, so it has been adopted into state. "+
				"Consider a longer `timeouts { create = … }` (or provider request_timeout), "+
				"or a lower -parallelism.",
			t.Kind, t.Name, id,
		),
	)
	return id, true
}

// adoptAfterTimeout finds the object a timed-out create produced. It polls the
// organization's list for one matching t, and returns its id when there is
// exactly one. Zero matches after the grace period means the server really did
// not create it (safe to retry); more than one means the practitioner must
// disambiguate with `terraform import`, so an error is returned rather than
// guessing.
func adoptAfterTimeout(ctx context.Context, t adoptTarget) (string, error) {
	deadline := time.Now().Add(adoptTimeoutGrace)
	for {
		matches, err := listAdoptCandidates(ctx, t)
		if err != nil {
			return "", fmt.Errorf("could not list %ss to check whether it was created anyway: %v. "+
				"Check the organization for a %s named %q before re-running; if it exists, "+
				"`terraform import` it to avoid creating a duplicate", t.Kind, err, t.Kind, t.Name)
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("no %s named %q appeared within %s, so it was not created; "+
					"re-run to retry, or raise request_timeout / lower -parallelism", t.Kind, t.Name, adoptTimeoutGrace)
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("gave up waiting for %s %q: %v", t.Kind, t.Name, ctx.Err())
			case <-time.After(adoptPollInterval):
			}
		default:
			return "", fmt.Errorf("%d %ss named %q were created since the request started (%s); "+
				"cannot tell which one this resource is. Import the right one with "+
				"`terraform import <address> <id>` and delete the others",
				len(matches), t.Kind, t.Name, strings.Join(matches, ", "))
		}
	}
}

// listAdoptCandidates pages through t.List and returns the ids of objects
// named t.Name (and of type t.Type, when set) created at or after the
// clock-skew-adjusted t.StartedAt. Objects without a parseable created_at are
// included, to err on the side of finding it.
func listAdoptCandidates(ctx context.Context, t adoptTarget) ([]string, error) {
	notBefore := t.StartedAt.Add(-time.Minute)
	var ids []string
	const pageSize int32 = 100
	for offset := int32(0); ; offset += pageSize {
		page, total, httpResp, err := t.List(ctx, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("%v (response: %s)", err, getResponseBody(httpResp))
		}
		for _, c := range page {
			if c.ID == nil || c.Name == nil || *c.Name != t.Name {
				continue
			}
			if t.Type != "" && (c.Type == nil || *c.Type != t.Type) {
				continue
			}
			if !t.AnyAge && c.CreatedAt != nil {
				if created, perr := time.Parse(time.RFC3339Nano, *c.CreatedAt); perr == nil && created.Before(notBefore) {
					continue
				}
			}
			ids = append(ids, *c.ID)
		}
		if len(page) < int(pageSize) {
			break
		}
		if total != nil && offset+pageSize >= *total {
			break
		}
	}
	return ids, nil
}

func isNotFoundResponse(resp *http.Response, body []byte) bool {
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusNotFound, http.StatusGone:
			return true
		}
	}
	return bytes.Contains(body, []byte("An item of this type does not exist"))
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

// reconcileOptionalString refreshes an Optional string scalar from the API
// without introducing a diff the practitioner never authored. api is the plain
// string value as returned by the SDK's nil-safe getters (e.g.
// GetDescription()), which yield "" both for an absent field and for one the
// API echoed as "". An omitted attribute is null in config and plan, so an API
// "" maps to null — unless prior state already holds "", meaning the
// practitioner wrote `description = ""` explicitly, in which case "" is kept so
// the two stay equal. Without the first rule Read stored "" against a null
// plan, which surfaced as a spurious `"" -> null` update and then a "Provider
// produced inconsistent result after apply" (ENG-9867); without the second an
// explicit "" would churn on every plan.
func reconcileOptionalString(prior types.String, api string) types.String {
	if api == "" {
		if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() == "" {
			return prior
		}
		return types.StringNull()
	}
	return types.StringValue(api)
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
