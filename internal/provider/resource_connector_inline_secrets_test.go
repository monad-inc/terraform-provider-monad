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

	"github.com/hashicorp/terraform-plugin-framework/diag"
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

// reuseAndApply runs both halves of an in-place reuse the way Update does,
// with the component update between them assumed to have succeeded. The last
// apply is taken to have sent the same slots inline as configured now.
func reuseAndApply(
	ctx context.Context, c *client.Client, configured, stored map[string]any, selfID string,
) (map[string]any, diag.Diagnostics) {
	return reuseAndApplyOwned(ctx, c, configured, stored, selfID, collectInlineSlots(configured))
}

func reuseAndApplyOwned(
	ctx context.Context, c *client.Client, configured, stored map[string]any, selfID string, owned inlineSlots,
) (map[string]any, diag.Diagnostics) {
	out, updates, diags := reuseInlineSecretsInPlace(ctx, c, "input", configured, stored, selfID, owned)
	if diags.HasError() {
		return out, diags
	}
	diags.Append(applyInPlaceSecretUpdates(ctx, c, "input", updates)...)
	return out, diags
}

func TestReuseInlineSecretsInPlace(t *testing.T) {
	ctx := context.Background()

	t.Run("same name updates the referenced secret and sends a reference", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID, "name": "me"})

		configured := map[string]any{"access_key": inline("archive-ak", "AKIANEW", "reader key")}
		got, diags := reuseAndApply(ctx, f.client(), configured,
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

	t.Run("the secret write is deferred to the caller", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID})

		configured := map[string]any{"access_key": inline("archive-ak", "AKIANEW", "reader key")}
		got, updates, diags := reuseInlineSecretsInPlace(ctx, f.client(), "input", configured,
			map[string]any{"access_key": ref("sec-1")}, selfID, collectInlineSlots(configured))

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, ref("sec-1"), got["access_key"])
		assert.Empty(t, f.updates, "nothing is written before the component update succeeds")
		require.Len(t, updates, 1)
		assert.Equal(t, "sec-1", updates[0].id)
	})

	t.Run("renamed secret is sent inline so the API creates it", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak-v1")

		want := inline("archive-ak-v2", "AKIANEW", "reader key")
		got, diags := reuseAndApply(ctx, f.client(),
			map[string]any{"access_key": want}, map[string]any{"access_key": ref("sec-1")}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Empty(t, f.updates)
	})

	t.Run("slot with no stored reference is sent inline without a read", func(t *testing.T) {
		f := newFakeSecretsServer(t)

		want := inline("new-key", "v", "d")
		got, diags := reuseAndApply(ctx, f.client(),
			map[string]any{"access_key": want}, map[string]any{}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Zero(t, f.gets)
	})

	t.Run("referenced secret deleted out of band is recreated inline", func(t *testing.T) {
		f := newFakeSecretsServer(t)

		want := inline("archive-ak", "v", "d")
		got, diags := reuseAndApply(ctx, f.client(),
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
		got, diags := reuseAndApply(ctx, f.client(), configured, map[string]any{
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

		_, diags := reuseAndApply(ctx, f.client(),
			map[string]any{"access_key": inline("shared-key", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.False(t, diags.HasError(), "%v", diags)
		require.Len(t, diags.Warnings(), 1)
		assert.Contains(t, diags.Warnings()[0].Detail(), `input "Other input"`)
		assert.NotContains(t, diags.Warnings()[0].Detail(), `"me"`)
	})

	t.Run("slot that held a reference at the last apply is refused", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-k", "aws-key", map[string]any{"id": selfID})

		_, diags := reuseAndApplyOwned(ctx, f.client(),
			map[string]any{"access_key": inline("aws-key", "v", "d")},
			map[string]any{"access_key": ref("sec-k")}, selfID, inlineSlots{})

		require.True(t, diags.HasError())
		assert.Equal(t, "Secret not owned by this input", diags.Errors()[0].Summary())
		assert.Contains(t, diags.Errors()[0].Detail(), `referenced secret "aws-key" (sec-k) by id`)
		assert.Empty(t, f.updates, "a secret this component did not create is never written")
	})

	t.Run("no record and a shared secret is refused", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "shared-key",
			map[string]any{"id": selfID, "name": "me"},
			map[string]any{"id": "input-other", "name": "Other input"})

		_, diags := reuseAndApplyOwned(ctx, f.client(),
			map[string]any{"access_key": inline("shared-key", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID, nil)

		require.True(t, diags.HasError())
		assert.Equal(t, "Shared secret not updated", diags.Errors()[0].Summary())
		assert.Contains(t, diags.Errors()[0].Detail(), `input "Other input"`)
		assert.Contains(t, diags.Errors()[0].Detail(), `{ id = "sec-1" }`)
		assert.Empty(t, f.updates)
	})

	t.Run("no record and an unshared secret is updated in place", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID})

		got, diags := reuseAndApplyOwned(ctx, f.client(),
			map[string]any{"access_key": inline("archive-ak", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID, nil)

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, ref("sec-1"), got["access_key"])
		require.Len(t, f.updates, 1)
	})

	t.Run("nested slot ownership is checked by its full path", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-n", "nested-key", map[string]any{"id": selfID})

		configured := map[string]any{"auth": map[string]any{"token": inline("nested-key", "v", "d")}}
		stored := map[string]any{"auth": map[string]any{"token": ref("sec-n")}}

		_, diags := reuseAndApplyOwned(ctx, f.client(), configured, stored, selfID,
			collectInlineSlots(map[string]any{"token": inline("nested-key", "v", "d")}))
		require.True(t, diags.HasError(), "a top-level token slot does not own auth.token")

		_, diags = reuseAndApplyOwned(ctx, f.client(), configured, stored, selfID, collectInlineSlots(configured))
		require.False(t, diags.HasError(), "%v", diags)
		require.Len(t, f.updates, 1)
	})

	t.Run("read error surfaces", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak")
		f.failGet = true

		_, diags := reuseAndApply(ctx, f.client(),
			map[string]any{"access_key": inline("archive-ak", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.True(t, diags.HasError())
		assert.Contains(t, diags.Errors()[0].Detail(), "Unable to read secret sec-1")
	})

	t.Run("update error surfaces", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak")
		f.failPut = true

		_, diags := reuseAndApply(ctx, f.client(),
			map[string]any{"access_key": inline("archive-ak", "v", "d")},
			map[string]any{"access_key": ref("sec-1")}, selfID)

		require.True(t, diags.HasError())
		assert.Contains(t, diags.Errors()[0].Detail(), `The input was updated, but updating secret "archive-ak"`)
	})
}

func TestSecretsForUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("no inline secrets skips the component read", func(t *testing.T) {
		configured := map[string]any{"secret_key": ref("sec-ref")}
		got, _, diags := secretsForUpdate(ctx, nil, "input", selfID, configured, true, nil,
			func(context.Context) (map[string]any, *http.Response, error) {
				t.Fatal("component read when no inline secret is configured")
				return nil, nil, nil
			})
		require.False(t, diags.HasError())
		assert.Equal(t, configured, got)
	})

	t.Run("component read error surfaces", func(t *testing.T) {
		_, _, diags := secretsForUpdate(ctx, nil, "input", selfID,
			map[string]any{"access_key": inline("k", "v", "d")}, true, nil,
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
		got, _, diags := secretsForUpdate(ctx, f.client(), "input", selfID, configured, false, nil,
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
		got, _, diags := secretsForUpdate(ctx, f.client(), "input", selfID,
			map[string]any{"access_key": want}, false, nil, stored(map[string]any{}))

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, want, got["access_key"])
		assert.Zero(t, f.gets)
	})

	t.Run("changed secrets update the referenced secret in place", func(t *testing.T) {
		f := newFakeSecretsServer(t)
		f.add("sec-1", "archive-ak", map[string]any{"id": selfID})

		got, updates, diags := secretsForUpdate(ctx, f.client(), "input", selfID,
			map[string]any{"access_key": inline("archive-ak", "AKIANEW", "reader key")}, true,
			inlineSlots{`["access_key"]`: {}}, stored(map[string]any{"access_key": ref("sec-1")}))

		require.False(t, diags.HasError(), "%v", diags)
		assert.Equal(t, ref("sec-1"), got["access_key"])
		assert.Empty(t, f.updates, "the write waits for applyInPlaceSecretUpdates")
		require.Len(t, updates, 1)
		assert.Equal(t, "sec-1", updates[0].id)
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

// fakePrivate stands in for the framework's private state data.
type fakePrivate map[string][]byte

func (p fakePrivate) GetKey(_ context.Context, key string) ([]byte, diag.Diagnostics) {
	return p[key], nil
}

func (p fakePrivate) SetKey(_ context.Context, key string, value []byte) diag.Diagnostics {
	p[key] = value
	return nil
}

func TestInlineSlotsPrivateState(t *testing.T) {
	ctx := context.Background()

	t.Run("no record reads as nil", func(t *testing.T) {
		got, diags := readInlineSlots(ctx, fakePrivate{})
		require.False(t, diags.HasError())
		assert.Nil(t, got)
	})

	t.Run("round trip records only inline slots", func(t *testing.T) {
		p := fakePrivate{}
		require.False(t, writeInlineSlots(ctx, p, map[string]any{
			"access_key": inline("a", "v", "d"),
			"secret_key": ref("sec-ref"),
			"auth":       map[string]any{"token": inline("t", "v", "d"), "other": ref("x")},
		}).HasError())
		assert.JSONEq(t, `[["access_key"],["auth","token"]]`, string(p[inlineSecretSlotsKey]))

		got, diags := readInlineSlots(ctx, p)
		require.False(t, diags.HasError())
		assert.True(t, got.has([]string{"access_key"}))
		assert.True(t, got.has([]string{"auth", "token"}))
		assert.False(t, got.has([]string{"secret_key"}))
		assert.False(t, got.has([]string{"auth", "other"}))
	})

	t.Run("no inline slots is still a record", func(t *testing.T) {
		p := fakePrivate{}
		require.False(t, writeInlineSlots(ctx, p, map[string]any{"k": ref("x")}).HasError())
		got, diags := readInlineSlots(ctx, p)
		require.False(t, diags.HasError())
		assert.NotNil(t, got)
		assert.False(t, got.has([]string{"k"}))
	})

	t.Run("unreadable record warns and reads as nil", func(t *testing.T) {
		got, diags := readInlineSlots(ctx, fakePrivate{inlineSecretSlotsKey: []byte(`{"bad":1}`)})
		require.False(t, diags.HasError())
		assert.Len(t, diags.Warnings(), 1)
		assert.Nil(t, got)
	})
}
