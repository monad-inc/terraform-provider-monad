package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestSchemasValidateImplementation asserts that every resource schema passes
// the framework's own implementation validation. This is what catches an
// illegal write-only placement (e.g. write-only inside a set nested block) or a
// mis-declared computed attribute, so it guards the config.secrets WriteOnly +
// config.secrets_hash Computed design.
func TestSchemasValidateImplementation(t *testing.T) {
	ctx := context.Background()

	resources := map[string]resource.Resource{
		"input":      &ResourceInput{},
		"output":     &ResourceOutput{},
		"enrichment": &ResourceEnrichment{},
		"transform":  &ResourceTransform{},
		"pipeline":   &ResourcePipeline{},
		"secret":     &ResourceSecret{},
	}

	for name, r := range resources {
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s schema build failed: %s", name, resp.Diagnostics)
			continue
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s schema invalid: %s", name, diags)
		}
	}
}

func TestComputeSecretsHash(t *testing.T) {
	ctx := context.Background()

	// Empty secrets produce no hash.
	if h, err := computeSecretsHash(ctx, "org", nil); err != nil || h != "" {
		t.Fatalf("empty secrets: got (%q, %v), want (\"\", nil)", h, err)
	}

	// Same data, different key ordering, hashes identically (json sorts keys).
	a, err := computeSecretsHash(ctx, "org", map[string]any{"token": "abc", "user": "x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := computeSecretsHash(ctx, "org", map[string]any{"user": "x", "token": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("hash not order-independent: %q != %q", a, b)
	}

	// A changed value changes the hash (rotation is detectable).
	c, err := computeSecretsHash(ctx, "org", map[string]any{"token": "abc", "user": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Error("hash did not change when a secret value changed")
	}

	// A different key produces a different hash for the same data.
	d, err := computeSecretsHash(ctx, "other-org", map[string]any{"token": "abc", "user": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if a == d {
		t.Error("hash did not depend on the key")
	}
}

func TestDynamicsSemanticallyEqual(t *testing.T) {
	// int64 vs float64 for the same number, and nested slices, compare equal.
	if !dynamicsSemanticallyEqual(
		map[string]any{"n": int64(1), "l": []any{"a", "b"}},
		map[string]any{"n": float64(1), "l": []any{"a", "b"}},
	) {
		t.Error("numeric type churn should compare equal")
	}
	// nil and empty map are equal.
	if !dynamicsSemanticallyEqual(nil, map[string]any{}) {
		t.Error("nil and empty map should compare equal")
	}
	// Genuinely different data is not equal.
	if dynamicsSemanticallyEqual(
		map[string]any{"a": "1"},
		map[string]any{"a": "2"},
	) {
		t.Error("different data should not compare equal")
	}
	// An empty-string field the API omits compares equal to the practitioner's
	// explicit "" (the API drops empty values via omitempty). This is the
	// transform `description: ""` case.
	if !dynamicsSemanticallyEqual(
		map[string]any{"operations": []any{map[string]any{"op": "drop", "description": ""}}},
		map[string]any{"operations": []any{map[string]any{"op": "drop"}}},
	) {
		t.Error("explicit empty string should compare equal to an omitted field")
	}
	// But a real value change is still detected, not masked by pruning.
	if dynamicsSemanticallyEqual(
		map[string]any{"op": "drop", "description": "was here"},
		map[string]any{"op": "drop"},
	) {
		t.Error("a non-empty value vs absent should be detected as drift")
	}
}

func TestReconcileDynamicPreservesPriorType(t *testing.T) {
	// A tuple-typed dynamic (heterogeneous, like a jsondecode result) that is
	// semantically equal to the API map must be returned verbatim, preserving
	// its cty type.
	tuple, diags := types.TupleValue(
		[]attr.Type{types.StringType, types.Int64Type},
		[]attr.Value{types.StringValue("a"), types.Int64Value(2)},
	)
	if diags.HasError() {
		t.Fatal(diags)
	}
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"ops": types.TupleType{ElemTypes: []attr.Type{types.StringType, types.Int64Type}}},
		map[string]attr.Value{"ops": tuple},
	)
	if diags.HasError() {
		t.Fatal(diags)
	}
	prior := types.DynamicValue(obj)

	api := map[string]any{"ops": []any{"a", int64(2)}}

	got, err := reconcileDynamic(prior, api)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(prior) {
		t.Errorf("expected prior value preserved, got %#v", got)
	}

	// Genuine drift adopts the API value.
	drifted := map[string]any{"ops": []any{"a", int64(3)}}
	got, err = reconcileDynamic(prior, drifted)
	if err != nil {
		t.Fatal(err)
	}
	if got.Equal(prior) {
		t.Error("expected drift to replace prior value")
	}
}

func TestReconcilePipelineNodesMasksOmittedSlug(t *testing.T) {
	// Prior state omitted the slug (null); the API returns a generated slug.
	// This must NOT read as drift — prior is preserved verbatim.
	prior := []ResourcePipelineNode{{
		ComponentType: types.StringValue("input"),
		ComponentID:   types.StringValue("c1"),
		Slug:          types.StringNull(),
	}}
	api := []ResourcePipelineNode{{
		ComponentType: types.StringValue("input"),
		ComponentID:   types.StringValue("c1"),
		Slug:          types.StringValue("server-generated"),
	}}

	got := reconcilePipelineNodes(prior, api)
	if !got[0].Slug.IsNull() {
		t.Errorf("expected omitted slug preserved as null, got %v", got[0].Slug)
	}

	// A changed component IS drift and is adopted.
	drift := []ResourcePipelineNode{{
		ComponentType: types.StringValue("input"),
		ComponentID:   types.StringValue("c2"),
		Slug:          types.StringValue("server-generated"),
	}}
	got = reconcilePipelineNodes(prior, drift)
	if got[0].ComponentID.ValueString() != "c2" {
		t.Error("expected genuine node drift to be adopted")
	}
}

func TestReconcilePipelineNodesImportPopulates(t *testing.T) {
	// On import prior state is empty; the API view populates.
	api := []ResourcePipelineNode{{
		ComponentType: types.StringValue("input"),
		ComponentID:   types.StringValue("c1"),
		Slug:          types.StringValue("s1"),
	}}
	got := reconcilePipelineNodes(nil, api)
	if len(got) != 1 || got[0].ComponentID.ValueString() != "c1" {
		t.Errorf("expected API nodes on import, got %#v", got)
	}
}

func TestReconcilePipelineEdgesMasksOmittedNameDescription(t *testing.T) {
	prior := []ResourcePipelineEdge{{
		Name:                 types.StringNull(),
		Description:          types.StringNull(),
		FromNodeInstanceSlug: types.StringValue("a"),
		ToNodeInstanceSlug:   types.StringValue("b"),
		Condition:            ResourcePipelineCondition{Operator: types.StringValue("and")},
	}}
	api := []ResourcePipelineEdge{{
		Name:                 types.StringValue("edge-1"),
		Description:          types.StringValue("server desc"),
		FromNodeInstanceSlug: types.StringValue("a"),
		ToNodeInstanceSlug:   types.StringValue("b"),
		Condition:            ResourcePipelineCondition{Operator: types.StringValue("and")},
	}}

	got := reconcilePipelineEdges(prior, api)
	if !got[0].Name.IsNull() || !got[0].Description.IsNull() {
		t.Errorf("expected omitted name/description preserved as null, got name=%v desc=%v", got[0].Name, got[0].Description)
	}

	// A changed routing target IS drift.
	drift := []ResourcePipelineEdge{{
		Name:                 types.StringNull(),
		Description:          types.StringNull(),
		FromNodeInstanceSlug: types.StringValue("a"),
		ToNodeInstanceSlug:   types.StringValue("c"),
		Condition:            ResourcePipelineCondition{Operator: types.StringValue("and")},
	}}
	got = reconcilePipelineEdges(prior, drift)
	if got[0].ToNodeInstanceSlug.ValueString() != "c" {
		t.Error("expected genuine edge drift to be adopted")
	}
}
