package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectorSchemas are the two schemas that declare config.secrets_hash:
// input/output share getConnectorSchema, enrichment has its own.
func connectorSchemas(t *testing.T) map[string]schema.Schema {
	t.Helper()
	out := map[string]schema.Schema{}
	for name, r := range map[string]resource.Resource{
		"input":      &ResourceInput{},
		"enrichment": &ResourceEnrichment{},
	} {
		var sr resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
		require.False(t, sr.Diagnostics.HasError(), sr.Diagnostics)
		out[name] = sr.Schema
	}
	return out
}

// connectorValues builds a connector object whose config block carries the
// given secrets and secrets_hash; every other attribute is fixed or null.
func connectorValues(t *testing.T, typ tftypes.Object, secrets, hash tftypes.Value) tftypes.Value {
	t.Helper()
	configType := typ.AttributeTypes["config"].(tftypes.Object)
	return tftypes.NewValue(typ, map[string]tftypes.Value{
		"timeouts":    tftypes.NewValue(typ.AttributeTypes["timeouts"], nil),
		"id":          tftypes.NewValue(tftypes.String, "conn-id"),
		"name":        tftypes.NewValue(tftypes.String, "example"),
		"description": tftypes.NewValue(tftypes.String, nil),
		"type":        tftypes.NewValue(tftypes.String, "demo"),
		"config": tftypes.NewValue(configType, map[string]tftypes.Value{
			"settings":     tftypes.NewValue(tftypes.DynamicPseudoType, nil),
			"secrets":      secrets,
			"secrets_hash": hash,
		}),
	})
}

// Every diff on a connector must not drag secrets_hash to "known after apply"
// (the post-import `timeouts` diff did, ENG-10511): the attribute keeps state
// unless ModifyPlan marks it unknown.
func TestConnectorSecretsHashUsesStateForUnknown(t *testing.T) {
	for name, s := range connectorSchemas(t) {
		t.Run(name, func(t *testing.T) {
			blk, ok := s.Blocks["config"].(schema.SingleNestedBlock)
			require.True(t, ok, "config is a SingleNestedBlock")
			attr, ok := blk.Attributes["secrets_hash"].(schema.StringAttribute)
			require.True(t, ok, "secrets_hash is a StringAttribute")
			require.Len(t, attr.PlanModifiers, 1)
			assert.Equal(t, "Once set, the value of this attribute in state will not change.",
				attr.PlanModifiers[0].Description(context.Background()))
		})
	}
}

// TestConnectorModifyPlanForSecrets covers rotation detection for the
// write-only config.secrets, including the post-import and unknown-at-plan
// shapes.
func TestConnectorModifyPlanForSecrets(t *testing.T) {
	ctx := context.Background()
	const org = "org"

	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	nullStr := tftypes.NewValue(tftypes.String, nil)
	nullDyn := tftypes.NewValue(tftypes.DynamicPseudoType, nil)
	secretsType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_key": tftypes.String}}
	secretsOf := func(v tftypes.Value) tftypes.Value {
		return tftypes.NewValue(secretsType, map[string]tftypes.Value{"api_key": v})
	}

	storedHash, err := computeSecretsHash(ctx, org, map[string]any{"api_key": "current"})
	require.NoError(t, err)

	cases := []struct {
		name        string
		secrets     tftypes.Value // configured secrets
		stateHash   tftypes.Value
		wantUnknown bool
	}{
		{"unchanged secrets keep the stored hash", secretsOf(str("current")), str(storedHash), false},
		{"rotated secrets mark the hash unknown", secretsOf(str("rotated")), str(storedHash), true},
		{"removed secrets mark the hash unknown", nullDyn, str(storedHash), true},
		{"import without secrets stays null", nullDyn, nullStr, false},
		{"import with secrets marks the hash unknown", secretsOf(str("current")), nullStr, true},
		{"wholly unknown secrets mark the hash unknown", tftypes.NewValue(tftypes.DynamicPseudoType, tftypes.UnknownValue), nullStr, true},
		{"partly unknown secrets mark the hash unknown", secretsOf(tftypes.NewValue(tftypes.String, tftypes.UnknownValue)), str(storedHash), true},
	}

	for name, s := range connectorSchemas(t) {
		typ := s.Type().TerraformType(ctx).(tftypes.Object)
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				state := connectorValues(t, typ, nullDyn, tc.stateHash)
				// UseStateForUnknown has already run, so the plan carries
				// state's hash; write-only secrets are null in the plan.
				plan := connectorValues(t, typ, nullDyn, tc.stateHash)
				req := resource.ModifyPlanRequest{
					Config: tfsdk.Config{Schema: s, Raw: connectorValues(t, typ, tc.secrets, nullStr)},
					State:  tfsdk.State{Schema: s, Raw: state},
					Plan:   tfsdk.Plan{Schema: s, Raw: plan},
				}
				resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: plan}}

				modifyConnectorPlanForSecrets(ctx, org, req, &resp)
				require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
				assert.Zero(t, resp.Diagnostics.WarningsCount(), resp.Diagnostics)

				var got types.String
				require.False(t, resp.Plan.GetAttribute(ctx, path.Root("config").AtName("secrets_hash"), &got).HasError())
				assert.Equal(t, tc.wantUnknown, got.IsUnknown(), "secrets_hash unknown")
				if !tc.wantUnknown {
					var want types.String
					require.False(t, req.State.GetAttribute(ctx, path.Root("config").AtName("secrets_hash"), &want).HasError())
					assert.Equal(t, want, got, "hash preserved from state")
				}
			})
		}
	}
}
