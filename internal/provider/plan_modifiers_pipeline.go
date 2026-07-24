package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// normalizedComparableKeys renders each comparable element to a canonical JSON
// string with empty/omitted fields pruned, then sorts the results. Two
// collections that hold the same elements in any order — treating a null field
// the same as an omitted or empty one — produce identical key slices. It reuses
// the same jsonNormalize/pruneEmpty normalization the Read-path reconcile relies
// on, so "semantically equal" means the same thing in both places.
func normalizedComparableKeys(items []any) []string {
	keys := make([]string, len(items))
	for i, it := range items {
		b, err := json.Marshal(pruneEmpty(jsonNormalize(it)))
		if err != nil {
			// A marshal failure only makes the comparison stricter (this element
			// won't match), never wrong.
			keys[i] = ""
			continue
		}
		keys[i] = string(b)
	}
	sort.Strings(keys)
	return keys
}

// pipelineEdgesSetEqual reports whether two edge lists are the same multiset
// regardless of order.
func pipelineEdgesSetEqual(a, b []ResourcePipelineEdge) bool {
	return reflect.DeepEqual(
		normalizedComparableKeys(pipelineEdgesComparable(a)),
		normalizedComparableKeys(pipelineEdgesComparable(b)),
	)
}

// pipelineNodesSetEqual reports whether two node lists are the same multiset
// regardless of order.
func pipelineNodesSetEqual(a, b []ResourcePipelineNode) bool {
	return reflect.DeepEqual(
		normalizedComparableKeys(pipelineNodesComparable(a)),
		normalizedComparableKeys(pipelineNodesComparable(b)),
	)
}

// pipelineEdgesOrderInsensitive keeps the prior state edge list when the
// configured edges are the same set in a different order, so a reorder never
// shows as a diff. This matters most on the first plan after `terraform
// import`, where Read cannot reconstruct the practitioner's authored order and
// would otherwise reorder every edge (ENG-9221). Genuine edge changes still
// diff because the element sets differ.
type pipelineEdgesOrderInsensitive struct{}

var _ planmodifier.List = pipelineEdgesOrderInsensitive{}

func (pipelineEdgesOrderInsensitive) Description(context.Context) string {
	return "Suppresses a plan diff that only reorders an otherwise unchanged edge set."
}

func (m pipelineEdgesOrderInsensitive) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (pipelineEdgesOrderInsensitive) PlanModifyList(
	ctx context.Context,
	req planmodifier.ListRequest,
	resp *planmodifier.ListResponse,
) {
	// Nothing to preserve on create (no prior state) or when values aren't
	// fully known yet.
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsUnknown() {
		return
	}

	var stateEdges, configEdges []ResourcePipelineEdge
	if diags := req.StateValue.ElementsAs(ctx, &stateEdges, false); diags.HasError() {
		return
	}
	if diags := req.ConfigValue.ElementsAs(ctx, &configEdges, false); diags.HasError() {
		return
	}

	if pipelineEdgesSetEqual(stateEdges, configEdges) {
		resp.PlanValue = req.StateValue
	}
}

// pipelineNodesOrderInsensitive mirrors pipelineEdgesOrderInsensitive for nodes.
type pipelineNodesOrderInsensitive struct{}

var _ planmodifier.List = pipelineNodesOrderInsensitive{}

func (pipelineNodesOrderInsensitive) Description(context.Context) string {
	return "Suppresses a plan diff that only reorders an otherwise unchanged node set."
}

func (m pipelineNodesOrderInsensitive) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (pipelineNodesOrderInsensitive) PlanModifyList(
	ctx context.Context,
	req planmodifier.ListRequest,
	resp *planmodifier.ListResponse,
) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsUnknown() {
		return
	}

	var stateNodes, configNodes []ResourcePipelineNode
	if diags := req.StateValue.ElementsAs(ctx, &stateNodes, false); diags.HasError() {
		return
	}
	if diags := req.ConfigValue.ElementsAs(ctx, &configNodes, false); diags.HasError() {
		return
	}

	if pipelineNodesSetEqual(stateNodes, configNodes) {
		resp.PlanValue = req.StateValue
	}
}
