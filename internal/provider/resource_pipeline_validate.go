package provider

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithValidateConfig = &ResourcePipeline{}

// ValidateConfig checks each edge-condition leaf against its rule's schema.
//
// The API accepts a leaf whose config does not match its type_id and then
// routes nothing at runtime — no error at save, none in the node logs, none on
// the edge (ENG-9546). Catching it at plan turns that silent data-routing
// failure into a message the practitioner sees before anything is applied.
// ENG-9345 tracks the server-side half; this is the client-side complement.
func (r *ResourcePipeline) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var data ResourcePipelineModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for i, edge := range data.Edges {
		for j, leaf := range edge.Condition.Conditions {
			configPath := path.Root("edges").
				AtListIndex(i).
				AtName("condition").
				AtName("conditions").
				AtListIndex(j).
				AtName("config")

			validateConditionLeaf(leaf, configPath, resp)
		}
	}

	validatePipelineGraph(data, resp)
}

// Node types accepted in each graph position. Copied verbatim from
// core/pkg/pipeline_validation/rules.go — a client-side rule stricter than the
// server's would reject a pipeline the API would have accepted.
var (
	validRootNodeTypes        = []string{"input"}
	validMiddleNodeTypes      = []string{"transform", "enrichment"}
	validTerminatingNodeTypes = []string{"output"}
)

// validatePipelineGraph mirrors the topology checks the API runs in
// core/pkg/pipeline_validation/rules.go, so an invalid graph is reported at plan
// instead of as a 400 part-way through an apply — after other components in the
// same configuration have already been created.
func validatePipelineGraph(data ResourcePipelineModel, resp *resource.ValidateConfigResponse) {
	// Slugs are the identity edges reference. A node may omit its slug (the
	// server generates one), but such a node cannot be referenced by an edge.
	slugIndex := map[string]int{}
	duplicate := map[string]bool{}
	nodeType := map[string]string{}
	var knownSlugs []string

	for i, node := range data.Nodes {
		if node.Slug.IsNull() || node.Slug.IsUnknown() || node.Slug.ValueString() == "" {
			continue
		}
		slug := node.Slug.ValueString()
		if _, seen := slugIndex[slug]; seen {
			duplicate[slug] = true
			resp.Diagnostics.AddAttributeError(
				path.Root("nodes").AtListIndex(i).AtName("slug"),
				"Duplicate node slug",
				fmt.Sprintf(
					"Slug %q is used by more than one node. Slugs identify nodes to edges, so they "+
						"must be unique within a pipeline.", slug,
				),
			)
			continue
		}
		slugIndex[slug] = i
		knownSlugs = append(knownSlugs, slug)
		if !node.ComponentType.IsNull() && !node.ComponentType.IsUnknown() {
			nodeType[slug] = node.ComponentType.ValueString()
		}
	}

	// An edge referencing a slug no node declares is the most common authoring
	// error, and the graph checks below cannot run meaningfully without it.
	incoming := map[string][]string{}
	outgoing := map[string][]string{}
	resolvable := true

	for i, edge := range data.Edges {
		for _, end := range []struct {
			attr string
			val  types.String
		}{
			{"from_node_instance_slug", edge.FromNodeInstanceSlug},
			{"to_node_instance_slug", edge.ToNodeInstanceSlug},
		} {
			if end.val.IsNull() || end.val.IsUnknown() {
				resolvable = false
				continue
			}
			if _, ok := slugIndex[end.val.ValueString()]; !ok {
				resolvable = false
				resp.Diagnostics.AddAttributeError(
					path.Root("edges").AtListIndex(i).AtName(end.attr),
					"Unknown node slug",
					fmt.Sprintf(
						"No node in this pipeline declares slug %q. Known slugs: %s.",
						end.val.ValueString(), strings.Join(knownSlugs, ", "),
					),
				)
			}
		}

		if edge.FromNodeInstanceSlug.IsNull() || edge.ToNodeInstanceSlug.IsNull() ||
			edge.FromNodeInstanceSlug.IsUnknown() || edge.ToNodeInstanceSlug.IsUnknown() {
			continue
		}
		from := edge.FromNodeInstanceSlug.ValueString()
		to := edge.ToNodeInstanceSlug.ValueString()
		outgoing[from] = append(outgoing[from], to)
		incoming[to] = append(incoming[to], from)
	}

	// Topology checks need a resolvable graph; unresolved references would make
	// every one of them report noise on top of the real error.
	if !resolvable || len(duplicate) > 0 || len(knownSlugs) == 0 {
		return
	}

	// One incoming edge per node (validateUniqueTargets).
	for slug, sources := range incoming {
		if len(sources) > 1 {
			resp.Diagnostics.AddAttributeError(
				path.Root("nodes").AtListIndex(slugIndex[slug]).AtName("slug"),
				"Node has multiple incoming edges",
				fmt.Sprintf(
					"Node %q is the target of %d edges (from %s). Each node accepts at most one "+
						"incoming edge — give each branch its own node.",
					slug, len(sources), strings.Join(sources, ", "),
				),
			)
		}
	}

	// Exactly one root, and it must be an input (validateRootNode).
	var roots []string
	for _, slug := range knownSlugs {
		if len(incoming[slug]) == 0 {
			roots = append(roots, slug)
		}
	}
	switch {
	case len(roots) != 1:
		resp.Diagnostics.AddAttributeError(
			path.Root("nodes"),
			"Pipeline must have exactly one root node",
			fmt.Sprintf(
				"Found %d nodes with no incoming edge (%s). A pipeline starts at exactly one input "+
					"node; every other node must be the target of an edge.",
				len(roots), strings.Join(roots, ", "),
			),
		)
	case !slices.Contains(validRootNodeTypes, nodeType[roots[0]]):
		resp.Diagnostics.AddAttributeError(
			path.Root("nodes").AtListIndex(slugIndex[roots[0]]).AtName("component_type"),
			"Root node must be an input",
			fmt.Sprintf(
				"Node %q has no incoming edge, making it the pipeline's root, but its component_type "+
					"is %q. The root must be an input.", roots[0], nodeType[roots[0]],
			),
		)
	}

	for _, slug := range knownSlugs {
		hasIn := len(incoming[slug]) > 0
		hasOut := len(outgoing[slug]) > 0
		ctype := nodeType[slug]

		// Outputs are sinks (validateNoEdgesFromOutput). Attributed to the edge,
		// as core does — the node itself is fine, the edge is the mistake.
		if slices.Contains(validTerminatingNodeTypes, ctype) && hasOut {
			for i, edge := range data.Edges {
				if !edge.FromNodeInstanceSlug.IsNull() && edge.FromNodeInstanceSlug.ValueString() == slug {
					resp.Diagnostics.AddAttributeError(
						path.Root("edges").AtListIndex(i).AtName("from_node_instance_slug"),
						"Output nodes cannot have outgoing edges",
						fmt.Sprintf(
							"Node %q is an output, which is a sink. Remove this edge.", slug,
						),
					)
				}
			}
		}

		// A node in the middle must be a transform or enrichment
		// (validateMiddleNodeTypes).
		if hasIn && hasOut && ctype != "" && !slices.Contains(validMiddleNodeTypes, ctype) {
			resp.Diagnostics.AddAttributeError(
				path.Root("nodes").AtListIndex(slugIndex[slug]).AtName("component_type"),
				"Invalid node type for a middle node",
				fmt.Sprintf(
					"Node %q has both incoming and outgoing edges, so it must be a transform or "+
						"enrichment; got %q.", slug, ctype,
				),
			)
		}

		// Every branch must terminate at an output (validateTerminatingNodes).
		if ctype != "" && !slices.Contains(validTerminatingNodeTypes, ctype) && !hasOut {
			resp.Diagnostics.AddAttributeError(
				path.Root("nodes").AtListIndex(slugIndex[slug]).AtName("slug"),
				"Node does not lead to an output",
				fmt.Sprintf(
					"Node %q has no outgoing edge and is not an output, so records reaching it go "+
						"nowhere. Every branch must end at an output node.", slug,
				),
			)
		}
	}

	if cycle := findPipelineCycle(knownSlugs, outgoing); cycle != "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("edges"),
			"Circular path in pipeline",
			fmt.Sprintf(
				"The edges form a cycle (%s). Data flows forward only; a pipeline must be acyclic.",
				cycle,
			),
		)
	}
}

// findPipelineCycle mirrors core's validateNoCycles DFS, returning the offending
// path so the message can name it rather than just asserting a cycle exists.
func findPipelineCycle(slugs []string, outgoing map[string][]string) string {
	visited := map[string]bool{}
	stack := map[string]bool{}
	var trail []string
	var found string

	var dfs func(slug string) bool
	dfs = func(slug string) bool {
		visited[slug] = true
		stack[slug] = true
		trail = append(trail, slug)

		for _, next := range outgoing[slug] {
			if !visited[next] {
				if dfs(next) {
					return true
				}
			} else if stack[next] {
				found = strings.Join(append(trail, next), " -> ")
				return true
			}
		}

		stack[slug] = false
		trail = trail[:len(trail)-1]
		return false
	}

	for _, slug := range slugs {
		if !visited[slug] && dfs(slug) {
			return found
		}
	}
	return ""
}

func validateConditionLeaf(
	leaf ResourcePipelineConditionCondition,
	configPath path.Path,
	resp *resource.ValidateConfigResponse,
) {
	typeIDPath := configPath.ParentPath().AtName("type_id")

	// A value that comes from a variable or another resource is not knowable at
	// validate time; leave those to the API.
	if leaf.TypeID.IsUnknown() {
		return
	}

	if leaf.TypeID.IsNull() || leaf.TypeID.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			typeIDPath,
			"Missing condition type_id",
			"Every condition leaf needs a `type_id` naming the rule to evaluate "+
				"(for example \"equals\", \"equals_any\", \"key_exists\"). A leaf without one "+
				"is not evaluated and the edge routes nothing.",
		)
		return
	}

	typeID := leaf.TypeID.ValueString()
	spec, known := conditionRules[typeID]
	if !known {
		// Warn rather than error: a rule shipped by the API after this provider
		// was built should not break an otherwise valid configuration.
		resp.Diagnostics.AddAttributeWarning(
			typeIDPath,
			"Unrecognized condition type_id",
			fmt.Sprintf(
				"%q is not a rule this provider knows about, so its configuration cannot be "+
					"checked here. Known rules: %s. If the rule is new, this warning is safe to "+
					"ignore — every field you set will be sent as-is.",
				typeID, strings.Join(knownRuleNames(), ", "),
			),
		)
		return
	}

	for _, field := range spec.required {
		if _, set := conditionFieldValue(leaf.Config, field); !set {
			resp.Diagnostics.AddAttributeError(
				configPath.AtName(field),
				"Missing required condition field",
				fmt.Sprintf(
					"type_id %q requires %q. %s",
					typeID, field, fieldHint(typeID, field),
				),
			)
		}
	}

	for _, field := range allConditionFields {
		if spec.allows(field) {
			continue
		}
		if _, set := conditionFieldValue(leaf.Config, field); set {
			resp.Diagnostics.AddAttributeError(
				configPath.AtName(field),
				"Condition field not used by this rule",
				fmt.Sprintf(
					"type_id %q does not read %q, so setting it has no effect. %s",
					typeID, field, fieldHint(typeID, field),
				),
			)
		}
	}
}

// allConditionFields is every field the config block exposes, used to detect a
// field set on a rule that ignores it.
var allConditionFields = []string{
	fieldKey, fieldValue, fieldValues, fieldPattern, fieldPercent, fieldRate,
	fieldNot, fieldCaseInsensitive, fieldRaw, fieldNull, fieldWhitespaceString,
}

// fieldHint points at the right field for the mistakes that are easy to make:
// value vs values, and the rules that take neither.
func fieldHint(typeID, field string) string {
	switch {
	case typeID == "equals_any" && field == fieldValues:
		return "Use values = [\"a\", \"b\"] for equals_any; `value` is for the single-value rules."
	case typeID == "equals_any" && field == fieldValue:
		return "equals_any matches a set — use `values` instead."
	case field == fieldValue && typeID == "matches_regex":
		return "matches_regex takes `pattern`, not `value`."
	case field == fieldPattern:
		return "Only matches_regex reads `pattern`."
	case field == fieldPercent:
		return "Only sample reads `percent`; for example percent = 12.5."
	case field == fieldRate:
		return "`rate` belongs to the deprecated sample_rate rule. Prefer type_id = \"sample\" with `percent`."
	case field == fieldValue:
		return "Single-value rules are equals, contains, starts_with, ends_with, greater_than and less_than."
	case field == fieldCaseInsensitive:
		return "case_insensitive applies to equals, equals_any, contains, starts_with and ends_with."
	case field == fieldRaw:
		return "`raw` applies to contains only."
	case field == fieldNull || field == fieldWhitespaceString:
		return "This flag applies to is_empty only."
	case field == fieldNot:
		return "`not` is accepted by every rule except sample."
	case field == fieldKey:
		return "`key` names the record field to test."
	}
	return ""
}

func knownRuleNames() []string {
	names := make([]string, 0, len(conditionRules))
	for name := range conditionRules {
		if conditionRules[name].deprecated {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
