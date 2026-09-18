package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	monad "github.com/monad-inc/sdk/go"
)

func boolp(b bool) *bool { return &b }

func TestBuildSchemaDetectionRequest(t *testing.T) {
	// Omitted block -> explicit disabled.
	got := buildSchemaDetectionRequest(nil)
	if got == nil || *got.Enabled || *got.DisableAlerting {
		t.Fatalf("nil block should send {false,false}, got %+v", got)
	}
	// Block with only enabled set -> disable_alerting false.
	got = buildSchemaDetectionRequest(&ResourcePipelineSchemaDetection{Enabled: types.BoolValue(true), DisableAlerting: types.BoolNull()})
	if !*got.Enabled || *got.DisableAlerting {
		t.Fatalf("expected {true,false}, got %+v", got)
	}
	got = buildSchemaDetectionRequest(&ResourcePipelineSchemaDetection{Enabled: types.BoolValue(true), DisableAlerting: types.BoolValue(true)})
	if !*got.Enabled || !*got.DisableAlerting {
		t.Fatalf("expected {true,true}, got %+v", got)
	}
}

func TestSchemaDetectionFromAPI(t *testing.T) {
	if schemaDetectionFromAPI(nil) != nil {
		t.Fatal("nil spec should map to nil block")
	}
	if schemaDetectionFromAPI(&monad.ModelsSchemaDetection{Enabled: boolp(false), DisableAlerting: boolp(false)}) != nil {
		t.Fatal("all-false spec should map to nil block (the omitted-block shape)")
	}
	got := schemaDetectionFromAPI(&monad.ModelsSchemaDetection{Enabled: boolp(true)})
	if got == nil || !got.Enabled.ValueBool() || !got.DisableAlerting.IsNull() {
		t.Fatalf("expected {true, null}: a false maps to the omitted shape, got %+v", got)
	}
	got = schemaDetectionFromAPI(&monad.ModelsSchemaDetection{Enabled: boolp(false), DisableAlerting: boolp(true)})
	if got == nil || !got.Enabled.IsNull() || !got.DisableAlerting.ValueBool() {
		t.Fatalf("expected {null, true}, got %+v", got)
	}
}

func edgeAB(spec *ResourcePipelineSchemaDetection) ResourcePipelineEdge {
	return ResourcePipelineEdge{
		Name:                 types.StringNull(),
		Description:          types.StringNull(),
		FromNodeInstanceSlug: types.StringValue("a"),
		ToNodeInstanceSlug:   types.StringValue("b"),
		Condition:            ResourcePipelineCondition{Operator: types.StringValue("always")},
		SchemaDetectionSpec:  spec,
	}
}

func TestReconcilePipelineEdgesSchemaDetection(t *testing.T) {
	// Detection switched on outside Terraform while the config omits the block:
	// that IS drift and must be adopted so the plan shows it.
	prior := []ResourcePipelineEdge{edgeAB(nil)}
	api := []ResourcePipelineEdge{edgeAB(&ResourcePipelineSchemaDetection{Enabled: types.BoolValue(true), DisableAlerting: types.BoolValue(false)})}
	got := reconcilePipelineEdges(prior, api)
	if got[0].SchemaDetectionSpec == nil || !got[0].SchemaDetectionSpec.Enabled.ValueBool() {
		t.Fatalf("expected server-side enablement to be adopted as drift, got %+v", got[0].SchemaDetectionSpec)
	}

	// An explicitly written all-false block is semantically the API's omitted
	// shape; keep the practitioner's block verbatim rather than nulling it.
	explicitOff := &ResourcePipelineSchemaDetection{Enabled: types.BoolValue(false), DisableAlerting: types.BoolValue(false)}
	got = reconcilePipelineEdges([]ResourcePipelineEdge{edgeAB(explicitOff)}, []ResourcePipelineEdge{edgeAB(nil)})
	if got[0].SchemaDetectionSpec == nil || got[0].SchemaDetectionSpec.Enabled.IsNull() {
		t.Fatalf("expected explicit all-false block to be preserved, got %+v", got[0].SchemaDetectionSpec)
	}

	// Enabled in both -> no drift, prior kept.
	on := &ResourcePipelineSchemaDetection{Enabled: types.BoolValue(true), DisableAlerting: types.BoolValue(false)}
	got = reconcilePipelineEdges([]ResourcePipelineEdge{edgeAB(on)}, []ResourcePipelineEdge{edgeAB(on)})
	if !edgesSemanticallyEqual(got[0], edgeAB(on)) {
		t.Fatal("expected identical enablement to compare equal")
	}

	// Disabled out-of-band while config says enabled -> drift adopted, plan will
	// show enabled false -> true.
	got = reconcilePipelineEdges([]ResourcePipelineEdge{edgeAB(on)}, []ResourcePipelineEdge{edgeAB(nil)})
	if got[0].SchemaDetectionSpec != nil {
		t.Fatalf("expected out-of-band disable to be adopted (nil block), got %+v", got[0].SchemaDetectionSpec)
	}
}
