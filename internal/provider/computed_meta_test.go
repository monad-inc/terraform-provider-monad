package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	monad "github.com/monad-inc/sdk/go"
)

// TestComputedMetaOnEveryResource pins ENG-11406: every resource exports the
// API's bookkeeping fields. created_at/managed_by keep their state value;
// updated_at must NOT (it changes on every write, so a kept value would make
// each update an apply-consistency error).
func TestComputedMetaOnEveryResource(t *testing.T) {
	ctx := context.Background()
	for _, newResource := range (&MonadProvider{}).Resources(ctx) {
		r := newResource()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "monad"}, &meta)
		var s resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &s)

		t.Run(meta.TypeName, func(t *testing.T) {
			created, ok := s.Schema.Attributes["created_at"].(schema.StringAttribute)
			require.True(t, ok, "created_at missing")
			assert.True(t, created.Computed)
			assert.False(t, created.Optional)
			assert.Len(t, created.PlanModifiers, 1, "created_at keeps its state value")

			updated, ok := s.Schema.Attributes["updated_at"].(schema.StringAttribute)
			require.True(t, ok, "updated_at missing")
			assert.True(t, updated.Computed)
			assert.Empty(t, updated.PlanModifiers, "updated_at must be unknown on change, never kept")

			// The API's secret, connection and API key models have no managed_by.
			noManagedBy := meta.TypeName == "monad_secret" || meta.TypeName == "monad_sso_connection" ||
				meta.TypeName == "monad_api_key"
			_, hasManagedBy := s.Schema.Attributes["managed_by"]
			assert.Equal(t, !noManagedBy, hasManagedBy, "managed_by only where the API model has it")
		})
	}
}

func TestMetaString(t *testing.T) {
	assert.True(t, metaString("").IsNull(), "absent maps to null, never \"\"")
	assert.Equal(t, "2026-10-22T00:00:00Z", metaString("2026-10-22T00:00:00Z").ValueString())

	assert.True(t, managedByString(nil).IsNull())
	customer := monad.ModelsManagedBy("customer")
	assert.Equal(t, "customer", managedByString(&customer).ValueString())
}
