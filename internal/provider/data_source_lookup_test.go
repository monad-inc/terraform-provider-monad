package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lookupSchemas(t *testing.T) map[string]datasource.SchemaResponse {
	t.Helper()
	out := map[string]datasource.SchemaResponse{}
	for _, n := range []func() datasource.DataSource{
		NewDataSourceInput, NewDataSourceOutput, NewDataSourceTransform,
		NewDataSourceEnrichment, NewDataSourcePipeline, NewDataSourceSecret,
	} {
		d := n()
		var meta datasource.MetadataResponse
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "monad"}, &meta)
		var s datasource.SchemaResponse
		d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
		require.False(t, s.Diagnostics.HasError())
		out[meta.TypeName] = s
	}
	return out
}

// TestLookupDataSourceAttributes pins which attributes each kind exposes; a
// secret never exposes a value.
func TestLookupDataSourceAttributes(t *testing.T) {
	want := map[string][]string{
		"monad_input":      {"type", "managed_by", "pipeline_ids"},
		"monad_output":     {"type", "managed_by", "pipeline_ids"},
		"monad_enrichment": {"type", "managed_by", "pipeline_ids"},
		"monad_transform":  {"managed_by", "pipeline_ids"},
		"monad_pipeline":   {"managed_by", "enabled"},
		"monad_secret":     {},
	}
	optional := []string{"type", "managed_by", "pipeline_ids", "enabled"}
	for name, s := range lookupSchemas(t) {
		t.Run(name, func(t *testing.T) {
			for _, a := range []string{"id", "name", "description", "created_at", "updated_at"} {
				assert.Contains(t, s.Schema.Attributes, a)
			}
			for _, a := range optional {
				_, has := s.Schema.Attributes[a]
				assert.Equal(t, contains(want[name], a), has, "attribute %s", a)
			}
			assert.NotContains(t, s.Schema.Attributes, "value")
		})
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestLookupValidateConfigExactlyOne(t *testing.T) {
	d := NewDataSourceInput().(*dataSourceLookup)
	var s datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	typ := s.Schema.Type().TerraformType(context.Background()).(tftypes.Object)

	cfg := func(id, name any) tfsdk.Config {
		vals := map[string]tftypes.Value{}
		for k, at := range typ.AttributeTypes {
			vals[k] = tftypes.NewValue(at, nil)
		}
		vals["id"] = tftypes.NewValue(tftypes.String, id)
		vals["name"] = tftypes.NewValue(tftypes.String, name)
		return tfsdk.Config{Schema: s.Schema, Raw: tftypes.NewValue(typ, vals)}
	}
	for _, c := range []struct {
		name    string
		id, nm  any
		wantErr bool
	}{
		{"by id", "abc", nil, false},
		{"by name", nil, "demo", false},
		{"neither", nil, nil, true},
		{"both", "abc", "demo", true},
	} {
		resp := &datasource.ValidateConfigResponse{}
		d.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{Config: cfg(c.id, c.nm)}, resp)
		assert.Equal(t, c.wantErr, resp.Diagnostics.HasError(), c.name)
	}
}
