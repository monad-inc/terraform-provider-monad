package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	monad "github.com/monad-inc/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ENG-11405: the API's condition is recursive up to depth 3 (edge operator ->
// group -> rules). The provider modelled only rules under the edge operator, so
// a group read back as an empty rule and import emitted invalid config.

func equalsRule(key, value string) ResourcePipelineConditionLeaf {
	cfg := nullConditionConfig()
	cfg.Key = types.StringValue(key)
	cfg.Value = types.StringValue(value)
	return ResourcePipelineConditionLeaf{TypeID: types.StringValue("equals"), Config: &cfg}
}

func keyExistsElement(key string) ResourcePipelineConditionCondition {
	cfg := nullConditionConfig()
	cfg.Key = types.StringValue(key)
	return ResourcePipelineConditionCondition{
		TypeID:     types.StringValue("key_exists"),
		Operator:   types.StringNull(),
		Config:     &cfg,
		Conditions: []ResourcePipelineConditionLeaf{},
	}
}

func orGroup(rules ...ResourcePipelineConditionLeaf) ResourcePipelineConditionCondition {
	return ResourcePipelineConditionCondition{
		TypeID:     types.StringNull(),
		Operator:   types.StringValue("or"),
		Conditions: rules,
	}
}

// nestedEdge is and(or(eventName=ConsoleLogin, eventName=AssumeRole), key_exists(userIdentity)),
// the shape from the ENG-11405 repro.
func nestedEdge() ResourcePipelineEdge {
	e := edgeAB(nil)
	e.Condition = ResourcePipelineCondition{
		Operator: types.StringValue("and"),
		Conditions: []ResourcePipelineConditionCondition{
			orGroup(equalsRule("eventName", "ConsoleLogin"), equalsRule("eventName", "AssumeRole")),
			keyExistsElement("userIdentity"),
		},
	}
	return e
}

func nestedAPICondition() *monad.ModelsConditionEvaluatable {
	and := monad.ModelsConditionOperator("and")
	or := monad.ModelsConditionOperator("or")
	return &monad.ModelsConditionEvaluatable{
		Operator: &and,
		Conditions: []monad.ModelsConditionEvaluatable{
			{
				Operator: &or,
				Conditions: []monad.ModelsConditionEvaluatable{
					{TypeId: monad.PtrString("equals"), Config: map[string]any{"key": "eventName", "value": "ConsoleLogin"}},
					{TypeId: monad.PtrString("equals"), Config: map[string]any{"key": "eventName", "value": "AssumeRole"}},
				},
			},
			{TypeId: monad.PtrString("key_exists"), Config: map[string]any{"key": "userIdentity"}},
		},
	}
}

func TestBuildPipelineRequestEdgesGroup(t *testing.T) {
	got, err := buildPipelineRequestEdges(context.Background(), []ResourcePipelineEdge{nestedEdge()})
	require.NoError(t, err)

	root := got[0].Conditions
	require.Len(t, root.Conditions, 2)

	group := root.Conditions[0]
	require.NotNil(t, group.Operator)
	assert.Equal(t, "or", string(*group.Operator))
	assert.Nil(t, group.TypeId, "a group sends no type_id")
	assert.Empty(t, group.Config, "a group sends no config")
	require.Len(t, group.Conditions, 2)
	assert.Equal(t, "equals", *group.Conditions[0].TypeId)
	assert.Equal(t, map[string]any{"key": "eventName", "value": "ConsoleLogin"}, group.Conditions[0].Config)

	rule := root.Conditions[1]
	assert.Nil(t, rule.Operator, "a rule sends no operator")
	assert.Equal(t, "key_exists", *rule.TypeId)
	assert.Equal(t, map[string]any{"key": "userIdentity"}, rule.Config)
}

func TestConditionFromAPIGroup(t *testing.T) {
	api := nestedAPICondition()

	group := conditionFromAPI(api.Conditions[0])
	assert.Equal(t, "or", group.Operator.ValueString())
	assert.True(t, group.TypeID.IsNull())
	assert.Nil(t, group.Config, "a group has no config block")
	require.Len(t, group.Conditions, 2)
	assert.Equal(t, "AssumeRole", group.Conditions[1].Config.Value.ValueString())

	rule := conditionFromAPI(api.Conditions[1])
	assert.True(t, rule.Operator.IsNull())
	assert.Equal(t, "key_exists", rule.TypeID.ValueString())
	require.NotNil(t, rule.Config)
	assert.NotNil(t, rule.Conditions, "a rule's nested list is empty, not null")
	assert.Empty(t, rule.Conditions)
}

func TestReconcilePipelineEdgesGroups(t *testing.T) {
	api := buildPipelineStateEdges(&monad.ModelsPipelineConfigV2{
		Nodes: []monad.ModelsPipelineNode{
			{Id: monad.PtrString("n1"), Slug: monad.PtrString("a")},
			{Id: monad.PtrString("n2"), Slug: monad.PtrString("b")},
		},
		Edges: []monad.ModelsPipelineEdge{{
			FromNodeInstanceId: monad.PtrString("n1"),
			ToNodeInstanceId:   monad.PtrString("n2"),
			Conditions:         nestedAPICondition(),
		}},
	})

	// Same tree in config and on the server: no drift, prior kept verbatim.
	got := reconcilePipelineEdges([]ResourcePipelineEdge{nestedEdge()}, api)
	assert.True(t, edgesSemanticallyEqual(got[0], nestedEdge()))

	// A rule inside the group changed server-side: drift is adopted.
	changed := nestedEdge()
	changed.Condition.Conditions[0].Conditions[1] = equalsRule("eventName", "GetObject")
	got = reconcilePipelineEdges([]ResourcePipelineEdge{changed}, api)
	assert.Equal(t, "AssumeRole", got[0].Condition.Conditions[0].Conditions[1].Config.Value.ValueString())

	// The group's operator changed server-side: drift is adopted.
	andInstead := nestedEdge()
	andInstead.Condition.Conditions[0].Operator = types.StringValue("and")
	got = reconcilePipelineEdges([]ResourcePipelineEdge{andInstead}, api)
	assert.Equal(t, "or", got[0].Condition.Conditions[0].Operator.ValueString())

	// Import (no prior): the API tree populates in full, group included.
	got = reconcilePipelineEdges(nil, api)
	assert.Equal(t, "or", got[0].Condition.Conditions[0].Operator.ValueString())
	assert.Len(t, got[0].Condition.Conditions[0].Conditions, 2)
}

func TestNormalizeConditionLists(t *testing.T) {
	// State from before groups existed decodes the nested list as nil.
	e := edgeAB(nil)
	rule := keyExistsElement("k")
	rule.Conditions = nil
	e.Condition = ResourcePipelineCondition{Operator: types.StringValue("and"), Conditions: []ResourcePipelineConditionCondition{rule}}
	edges := []ResourcePipelineEdge{e}
	normalizeConditionLists(edges)
	assert.NotNil(t, edges[0].Condition.Conditions[0].Conditions)
}

func validateElement(element ResourcePipelineConditionCondition) *resource.ValidateConfigResponse {
	resp := &resource.ValidateConfigResponse{}
	validateConditionElement(element, path.Root("edges").AtListIndex(0).AtName("condition").AtName("conditions").AtListIndex(0), resp)
	return resp
}

func TestValidateConditionElement(t *testing.T) {
	t.Run("valid group", func(t *testing.T) {
		resp := validateElement(orGroup(equalsRule("a", "1"), equalsRule("b", "2")))
		assert.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	})

	t.Run("valid rule", func(t *testing.T) {
		resp := validateElement(keyExistsElement("k"))
		assert.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	})

	t.Run("both type_id and operator errors", func(t *testing.T) {
		g := orGroup(equalsRule("a", "1"))
		g.TypeID = types.StringValue("equals")
		resp := validateElement(g)
		require.True(t, resp.Diagnostics.HasError())
		assert.Equal(t, "Condition is both a rule and a group", resp.Diagnostics.Errors()[0].Summary())
	})

	t.Run("group with config errors", func(t *testing.T) {
		g := orGroup(equalsRule("a", "1"))
		cfg := nullConditionConfig()
		cfg.Key = types.StringValue("a")
		g.Config = &cfg
		resp := validateElement(g)
		require.True(t, resp.Diagnostics.HasError())
		assert.Equal(t, "Condition group takes no config", resp.Diagnostics.Errors()[0].Summary())
	})

	t.Run("empty group errors", func(t *testing.T) {
		resp := validateElement(orGroup())
		require.True(t, resp.Diagnostics.HasError())
		assert.Equal(t, "Empty condition group", resp.Diagnostics.Errors()[0].Summary())
	})

	t.Run("empty always group is fine", func(t *testing.T) {
		g := orGroup()
		g.Operator = types.StringValue("always")
		resp := validateElement(g)
		assert.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	})

	t.Run("rule with nested conditions errors", func(t *testing.T) {
		r := keyExistsElement("k")
		r.Conditions = []ResourcePipelineConditionLeaf{equalsRule("a", "1")}
		resp := validateElement(r)
		require.True(t, resp.Diagnostics.HasError())
		assert.Equal(t, "Nested conditions need an operator", resp.Diagnostics.Errors()[0].Summary())
	})

	t.Run("rule inside a group is checked against its schema", func(t *testing.T) {
		bad := equalsRule("a", "1")
		bad.Config.Value = types.StringNull()
		resp := validateElement(orGroup(bad))
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "requires \"value\"")
	})
}
