package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	monad "github.com/monad-inc/sdk/go"
)

// ENG-11404: node `enabled` and edge `disabled` used to be absent from the
// model, so every update sent nodes as enabled and edges as not disabled,
// silently reverting a change made in the UI, and Read never surfaced it.

func nodeA(enabled types.Bool) ResourcePipelineNode {
	return ResourcePipelineNode{
		ComponentType: types.StringValue("input"),
		ComponentID:   types.StringValue("comp-a"),
		Slug:          types.StringValue("a"),
		Enabled:       enabled,
	}
}

func edgeABDisabled(disabled types.Bool) ResourcePipelineEdge {
	e := edgeAB(nil)
	e.Disabled = disabled
	return e
}

func TestBuildPipelineRequestNodesEnabled(t *testing.T) {
	cases := []struct {
		name    string
		enabled types.Bool
		want    bool
	}{
		{"omitted is enabled", types.BoolNull(), true},
		{"explicit true", types.BoolValue(true), true},
		{"explicit false", types.BoolValue(false), false},
	}
	for _, c := range cases {
		got := buildPipelineRequestNodes([]ResourcePipelineNode{nodeA(c.enabled)})
		if got[0].Enabled == nil || *got[0].Enabled != c.want {
			t.Errorf("%s: sent enabled=%v, want %v", c.name, got[0].Enabled, c.want)
		}
	}
}

func TestBuildPipelineRequestEdgesDisabled(t *testing.T) {
	cases := []struct {
		name     string
		disabled types.Bool
		want     bool
	}{
		{"omitted is not disabled", types.BoolNull(), false},
		{"explicit false", types.BoolValue(false), false},
		{"explicit true", types.BoolValue(true), true},
	}
	for _, c := range cases {
		got, err := buildPipelineRequestEdges(context.Background(), []ResourcePipelineEdge{edgeABDisabled(c.disabled)})
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Disabled == nil || *got[0].Disabled != c.want {
			t.Errorf("%s: sent disabled=%v, want %v", c.name, got[0].Disabled, c.want)
		}
	}
}

func TestBuildPipelineStateNodesEnabled(t *testing.T) {
	// The API default (enabled, or a missing field) maps to null so an omitted
	// attribute imports clean; only an actual `false` reaches state.
	for _, c := range []struct {
		name string
		api  *bool
		want types.Bool
	}{
		{"enabled", boolp(true), types.BoolNull()},
		{"missing", nil, types.BoolNull()},
		{"disabled", boolp(false), types.BoolValue(false)},
	} {
		p := &monad.ModelsPipelineConfigV2{Nodes: []monad.ModelsPipelineNode{{
			Id: monad.PtrString("n1"), Slug: monad.PtrString("a"), ComponentId: monad.PtrString("comp-a"), Enabled: c.api,
		}}}
		got := buildPipelineStateNodes(p)[0].Enabled
		if !got.Equal(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBuildPipelineStateEdgesDisabled(t *testing.T) {
	for _, c := range []struct {
		name string
		api  *bool
		want types.Bool
	}{
		{"not disabled", boolp(false), types.BoolNull()},
		{"missing", nil, types.BoolNull()},
		{"disabled", boolp(true), types.BoolValue(true)},
	} {
		p := &monad.ModelsPipelineConfigV2{
			Nodes: []monad.ModelsPipelineNode{
				{Id: monad.PtrString("n1"), Slug: monad.PtrString("a")},
				{Id: monad.PtrString("n2"), Slug: monad.PtrString("b")},
			},
			Edges: []monad.ModelsPipelineEdge{{
				FromNodeInstanceId: monad.PtrString("n1"), ToNodeInstanceId: monad.PtrString("n2"), Disabled: c.api,
			}},
		}
		got := buildPipelineStateEdges(p)[0].Disabled
		if !got.Equal(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReconcilePipelineNodesEnabled(t *testing.T) {
	// Disabled in the UI while config omits the attribute: drift, adopted so the
	// plan shows false -> null and the apply re-enables it deliberately.
	got := reconcilePipelineNodes([]ResourcePipelineNode{nodeA(types.BoolNull())}, []ResourcePipelineNode{nodeA(types.BoolValue(false))})
	if !got[0].Enabled.Equal(types.BoolValue(false)) {
		t.Fatalf("expected out-of-band disable to be adopted, got %v", got[0].Enabled)
	}

	// An explicit `enabled = true` is the API default read back as null: no
	// drift, and the practitioner's value is kept verbatim.
	got = reconcilePipelineNodes([]ResourcePipelineNode{nodeA(types.BoolValue(true))}, []ResourcePipelineNode{nodeA(types.BoolNull())})
	if !got[0].Enabled.Equal(types.BoolValue(true)) {
		t.Fatalf("expected explicit true to be preserved, got %v", got[0].Enabled)
	}

	// Disabled in config and on the server: no drift.
	got = reconcilePipelineNodes([]ResourcePipelineNode{nodeA(types.BoolValue(false))}, []ResourcePipelineNode{nodeA(types.BoolValue(false))})
	if !got[0].Enabled.Equal(types.BoolValue(false)) {
		t.Fatalf("expected disabled node to stay disabled, got %v", got[0].Enabled)
	}

	// Re-enabled in the UI while config says false: drift adopted.
	got = reconcilePipelineNodes([]ResourcePipelineNode{nodeA(types.BoolValue(false))}, []ResourcePipelineNode{nodeA(types.BoolNull())})
	if !got[0].Enabled.IsNull() {
		t.Fatalf("expected out-of-band enable to be adopted, got %v", got[0].Enabled)
	}
}

func TestReconcilePipelineEdgesDisabled(t *testing.T) {
	got := reconcilePipelineEdges([]ResourcePipelineEdge{edgeABDisabled(types.BoolNull())}, []ResourcePipelineEdge{edgeABDisabled(types.BoolValue(true))})
	if !got[0].Disabled.Equal(types.BoolValue(true)) {
		t.Fatalf("expected out-of-band disable to be adopted, got %v", got[0].Disabled)
	}

	got = reconcilePipelineEdges([]ResourcePipelineEdge{edgeABDisabled(types.BoolValue(false))}, []ResourcePipelineEdge{edgeABDisabled(types.BoolNull())})
	if !got[0].Disabled.Equal(types.BoolValue(false)) {
		t.Fatalf("expected explicit false to be preserved, got %v", got[0].Disabled)
	}

	got = reconcilePipelineEdges([]ResourcePipelineEdge{edgeABDisabled(types.BoolValue(true))}, []ResourcePipelineEdge{edgeABDisabled(types.BoolNull())})
	if !got[0].Disabled.IsNull() {
		t.Fatalf("expected out-of-band enable to be adopted, got %v", got[0].Disabled)
	}
}
