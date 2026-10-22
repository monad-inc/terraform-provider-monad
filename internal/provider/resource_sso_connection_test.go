package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ssoSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewResourceSSOConnection().(*ResourceSSOConnection).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError())
	return resp.Schema
}

func TestNormalizeSSOEmailDomain(t *testing.T) {
	assert.Equal(t, "example.com", normalizeSSOEmailDomain(" @Example.COM "))
	assert.Equal(t, "example.com", normalizeSSOEmailDomain("https://example.com/"))
	assert.Equal(t, "sub.example.com", normalizeSSOEmailDomain("sub.example.com"))
}

func TestSSOConnectionValidateConfig(t *testing.T) {
	ctx := context.Background()
	s := ssoSchema(t)
	typ := s.Type().TerraformType(ctx).(tftypes.Object)
	samlTyp := typ.AttributeTypes["saml"].(tftypes.Object)

	cfg := func(edit func(vals, saml map[string]tftypes.Value)) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for k, at := range typ.AttributeTypes {
			vals[k] = tftypes.NewValue(at, nil)
		}
		saml := map[string]tftypes.Value{
			"entity_id":    tftypes.NewValue(tftypes.String, "a8187bc5-d0c8-4d34-8448-180f6d0c95eb"),
			"metadata_url": tftypes.NewValue(tftypes.String, "https://idp.example.com/metadata"),
		}
		vals["name"] = tftypes.NewValue(tftypes.String, "okta")
		if edit != nil {
			edit(vals, saml)
		}
		vals["saml"] = tftypes.NewValue(samlTyp, saml)
		return tftypes.NewValue(typ, vals)
	}
	domains := func(d ...string) tftypes.Value {
		el := make([]tftypes.Value, len(d))
		for i, v := range d {
			el[i] = tftypes.NewValue(tftypes.String, v)
		}
		return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, el)
	}

	cases := []struct {
		name    string
		edit    func(vals, saml map[string]tftypes.Value)
		wantErr bool
	}{
		{"minimal", nil, false},
		{"full", func(v, s map[string]tftypes.Value) {
			v["email_domains"] = domains("example.com", "corp.example.com")
			v["public_name"] = tftypes.NewValue(tftypes.String, "Example SSO")
			v["session_timeout_minutes"] = tftypes.NewValue(tftypes.Number, 480)
		}, false},
		{"empty name", func(v, _ map[string]tftypes.Value) { v["name"] = tftypes.NewValue(tftypes.String, " ") }, true},
		{"domain not normalized", func(v, _ map[string]tftypes.Value) { v["email_domains"] = domains("@Example.com") }, true},
		{"domain invalid", func(v, _ map[string]tftypes.Value) { v["email_domains"] = domains("localhost") }, true},
		{"empty public_name", func(v, _ map[string]tftypes.Value) { v["public_name"] = tftypes.NewValue(tftypes.String, "") }, true},
		{"padded public_name", func(v, _ map[string]tftypes.Value) { v["public_name"] = tftypes.NewValue(tftypes.String, " x") }, true},
		{"session too short", func(v, _ map[string]tftypes.Value) {
			v["session_timeout_minutes"] = tftypes.NewValue(tftypes.Number, 29)
		}, true},
		{"session too long", func(v, _ map[string]tftypes.Value) {
			v["session_timeout_minutes"] = tftypes.NewValue(tftypes.Number, 1441)
		}, true},
		{"entity_id not a UUID", func(_, s map[string]tftypes.Value) { s["entity_id"] = tftypes.NewValue(tftypes.String, "urn:monad") }, true},
		{"metadata_url not https", func(_, s map[string]tftypes.Value) {
			s["metadata_url"] = tftypes.NewValue(tftypes.String, "http://idp.example.com/metadata")
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &resource.ValidateConfigResponse{}
			(&ResourceSSOConnection{}).ValidateConfig(ctx, resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: cfg(tc.edit)},
			}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
		})
	}
}

// TestSSOConnectionSchemaReplaceFields pins which fields force replacement:
// the API cannot update them.
func TestSSOConnectionSchemaReplaceFields(t *testing.T) {
	s := ssoSchema(t)
	assert.Len(t, s.Attributes["email_domains"].(schema.SetAttribute).PlanModifiers, 1)
	saml := s.Attributes["saml"].(schema.SingleNestedAttribute)
	entityID := saml.Attributes["entity_id"].(schema.StringAttribute)
	assert.True(t, entityID.Required, "entity_id must be Required: a generated one would change on replacement")
	assert.Len(t, entityID.PlanModifiers, 1)
	assert.Empty(t, saml.Attributes["metadata_url"].(schema.StringAttribute).PlanModifiers, "metadata_url updates in place")
}
