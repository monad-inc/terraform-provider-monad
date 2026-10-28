package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// fakeSecretsServer serves GET and update of /v2/{org}/secrets/{id} from an
// in-memory table and records every update it receives.
type fakeSecretsServer struct {
	srv *httptest.Server

	mu      sync.Mutex
	secrets map[string]map[string]any // id → GetSecret response body
	updates []secretUpdate
	gets    int
	failGet bool
	failPut bool
}

type secretUpdate struct {
	ID   string
	Body map[string]any
}

func newFakeSecretsServer(t *testing.T) *fakeSecretsServer {
	f := &fakeSecretsServer{secrets: map[string]map[string]any{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		const prefix = "/api/v2/org-1/secrets/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, prefix)
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet {
			f.gets++
			if f.failGet {
				http.Error(w, `{"code":500,"error":"boom"}`, http.StatusInternalServerError)
				return
			}
			s, ok := f.secrets[id]
			if !ok {
				http.Error(w, `{"code":404,"error":"secret not found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(s)
			return
		}

		if f.failPut {
			http.Error(w, `{"code":500,"error":"boom"}`, http.StatusInternalServerError)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.updates = append(f.updates, secretUpdate{ID: id, Body: body})
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": body["name"]})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSecretsServer) client() *client.Client {
	return client.NewMonadAPIClient(f.srv.URL, "tok", "org-1", true, 5*time.Second)
}

func (f *fakeSecretsServer) add(id, name string, inputs ...map[string]any) {
	f.secrets[id] = map[string]any{"id": id, "name": name, "inputs": inputs}
}

func inline(name, value, description string) map[string]any {
	return map[string]any{"name": name, "value": value, "description": description}
}

func ref(id string) map[string]any { return map[string]any{"id": id} }

const selfID = "input-self"

func TestReuseInlineSecretsInPlace(t *testing.T) {
	ctx := context.Background()

	t.Run("same name updates the referenced secret and sends a reference", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID, "name": "me"})

		configured := map[string]any{"access_key": inline("archive-ak", "AKIANEW", "reader key")}
		got, diags := reuseInlineSecretsInPlace(ctx, f.client(), configured,
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Empty(t, diags.Warnings(), "the only user is this component")
		assert.Equal(t, map[string]any{"access_key": ref("sec-1")}, got)
		require.Len(t, f.updates, 1)
		assert.Equal(t, "sec-1", f.updates[0].ID)
		assert.Equal(t, map[string]any{"name": "archive-ak", "value": "AKIANEW", "description": "reader key"},
			f.updates[0].Body)
		assert.Equal(t, inline("archive-ak", "AKIANEW", "reader key"), configured["access_key"],
			"configured secrets must not be modified; they are hashed")
	})

	t.Run("renamed secret is sent inline so the API creates it", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak-v1")

		want := inline("archive-ak-v2", "AKIANEW", "reader key")
		got, diags := reuseInlineSecretsInPlace(ctx, f.client(),
			map[string]any{"access_key": want}, map[string]any{"access_key": ref("sec-1")}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Empty(t, f.updates)
	})

	t.Run("slot with no stored reference is sent inline without a read", func(t *testing.T) {
		f := newFakeSecretsServer(t)

		want := inline("new-key", "v", "d")
		got, diags := reuseInlineSecretsInPlace(ctx, f.client(),
			map[string]any{"access_key": want}, map[string]any{}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Zero(t, f.gets)
	})

	t.Run("referenced secret deleted out of band is recreated inline", func(t *testing.T) {
		f := newFakeSecretsServer(t)

		want := inline("archive-ak", "v", "d")
		got, diags := reuseInlineSecretsInPlace(ctx, f.client(),
			map[string]any{"access_key": want}, map[string]any{"access_key": ref("gone")}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Empty(t, f.updates)
	})

	t.Run("references and nested slots", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-n", "nested-key", map[string]any{"id": selfID})

		configured := map[string]any{
			"secret_key": ref("sec-ref"),
			"auth":       map[string]any{"token": inline("nested-key", "v", "d")},
		}
		got, diags := reuseInlineSecretsInPlace(ctx, f.client(), configured, map[string]any{
			"secret_key": ref("sec-ref"),
			"auth":       map[string]any{"token": ref("sec-n")},
		}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, ref("sec-ref"), got["secret_key"], "references pass through untouched")
		assert.Equal(t, map[string]any{"token": ref("sec-n")}, got["auth"])
		require.Len(t, f.updates, 1)
		assert.Equal(t, "sec-n", f.updates[0].ID)
	})

	t.Run("shared secret warns and names the other users", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "shared-key",
			map[string]any{"id": selfID, "name": "me"},
			map[string]any{"id": "input-other", "name": "Other input"})

		_, diags := reuseInlineSecretsInPlace(ctx, f.client(),
			map[string]any{"access_key": inline("shared-key", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		require.Len(t, diags.Warnings(), 1)
		assert.Contains(t, diags.Warnings()[0].Detail(), `input "Other input"`)
		assert.NotContains(t, diags.Warnings()[0].Detail(), `"me"`)
	})

	t.Run("read error surfaces", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak")
		f.failGet = true

		_, diags := reuseInlineSecretsInPlace(ctx, f.client(),
			map[string]any{"access_key": inline("archive-ak", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.True(t, diags.HasError())
		assert.Contains(t, diags.Errors()[0].Detail(), "Unable to read secret sec-1")
	})

	t.Run("update error surfaces", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak")
		f.failPut = true

		_, diags := reuseInlineSecretsInPlace(ctx, f.client(),
			map[string]any{"access_key": inline("archive-ak", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.True(t, diags.HasError())
		assert.Contains(t, diags.Errors()[0].Detail(), `Unable to update secret "archive-ak"`)
	})
}

func TestSecretsForUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("no inline secrets skips the component read", func(t *testing.T) {
		configured := map[string]any{"secret_key": ref("sec-ref")}
		got, diags := secretsForUpdate(ctx, nil, "input", selfID, configured, true,
			func(context.Context) (map[string]any, *http.Response, error) {
				t.Fatal("component read when no inline secret is configured")
				return nil, nil, nil
			})
		require.False(t, diags.HasError())
		assert.Equal(t, configured, got)
	})

	t.Run("component read error surfaces", func(t *testing.T) {
		_, diags := secretsForUpdate(ctx, nil, "input", selfID,
			map[string]any{"access_key": inline("k", "v", "d")}, true,
			func(context.Context) (map[string]any, *http.Response, error) {
				return nil, nil, errors.New("connection refused")
			})
		require.True(t, diags.HasError())
		assert.Contains(t, diags.Errors()[0].Detail(), "Unable to read input before update")
	})

	stored := func(secrets map[string]any) storedSecretsFunc {
		return func(context.Context) (map[string]any, *http.Response, error) {
			return secrets, nil, nil
		}
	}

	t.Run("unchanged secrets are sent as stored references without a secret write", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID})
		f.add("sec-n", "nested-key", map[string]any{"id": selfID})

		configured := map[string]any{
			"access_key": inline("archive-ak", "AKIA", "reader key"),
			"secret_key": ref("sec-ref"),
			"auth":       map[string]any{"token": inline("nested-key", "v", "d")},
		}
		got, diags := secretsForUpdate(ctx, f.client(), "input", selfID, configured, false,
			stored(map[string]any{
				"access_key": ref("sec-1"),
				"secret_key": ref("sec-ref"),
				"auth":       map[string]any{"token": ref("sec-n")},
			}))

		require.False(t, diags.HasError(), "%v", diags)
		assert.Empty(t, diags.Warnings())
		assert.Equal(t, map[string]any{
			"access_key": ref("sec-1"),
			"secret_key": ref("sec-ref"),
			"auth":       map[string]any{"token": ref("sec-n")},
		}, got)
		assert.Zero(t, f.gets, "no secret read when the secrets did not change")
		assert.Empty(t, f.updates, "no secret write when the secrets did not change")
		assert.Equal(t, inline("archive-ak", "AKIA", "reader key"), configured["access_key"],
			"configured secrets must not be modified; they are hashed")
	})

	t.Run("unchanged inline secret with no stored reference is sent inline", func(t *testing.T) {
		f := newFakeSecretsServer(t)

		want := inline("archive-ak", "v", "d")
		got, diags := secretsForUpdate(ctx, f.client(), "input", selfID,
			map[string]any{"access_key": want}, false, stored(map[string]any{}))

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Zero(t, f.gets)
	})

	t.Run("changed secrets update the referenced secret in place", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID})

		got, diags := secretsForUpdate(ctx, f.client(), "input", selfID,
			map[string]any{"access_key": inline("archive-ak", "AKIANEW", "reader key")}, true,
			stored(map[string]any{"access_key": ref("sec-1")}))

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, ref("sec-1"), got["access_key"])
		require.Len(t, f.updates, 1)
		assert.Equal(t, "sec-1", f.updates[0].ID)
	})
}

func TestConnectorSecretsChanged(t *testing.T) {
	assert.False(t, connectorSecretsChanged(&ResourceConnectorModel{}), "no config block")
	assert.False(t, connectorSecretsChanged(&ResourceConnectorModel{
		Config: &ResourceConnectorConfig{SecretsHash: types.StringValue("h")},
	}), "stored hash kept: secrets unchanged")
	assert.False(t, connectorSecretsChanged(&ResourceConnectorModel{
		Config: &ResourceConnectorConfig{SecretsHash: types.StringNull()},
	}), "no secrets configured")
	assert.True(t, connectorSecretsChanged(&ResourceConnectorModel{
		Config: &ResourceConnectorConfig{SecretsHash: types.StringUnknown()},
	}), "hash unknown: secrets changed or first apply after import")
}

func TestHasInlineSecrets(t *testing.T) {
	assert.False(t, hasInlineSecrets(nil))
	assert.False(t, hasInlineSecrets(map[string]any{"a": ref("x")}))
	assert.False(t, hasInlineSecrets(map[string]any{"a": map[string]any{"name": "n", "value": "v"}}),
		"incomplete inline secret is not one the API accepts")
	assert.True(t, hasInlineSecrets(map[string]any{"a": inline("n", "v", "d")}))
	assert.True(t, hasInlineSecrets(map[string]any{"a": map[string]any{"b": inline("n", "v", "d")}}))
}
