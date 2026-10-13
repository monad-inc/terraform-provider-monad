package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// adoptFamily is one resource type's adopt-after-timeout wiring: where its
// list endpoint lives, the JSON key it returns items under, and the resource's
// own listForAdopt.
type adoptFamily struct {
	kind   string
	path   string // list endpoint path suffix
	key    string // response items key
	typed  bool   // adoptTarget.Type is set (connectors, alert rules)
	anyAge bool   // adoptTarget.AnyAge is set (secrets)
	lister func(c *client.Client) adoptLister
}

var adoptFamilies = []adoptFamily{
	{"pipeline", "/v2/org-1/pipelines", "pipelines", false, false,
		func(c *client.Client) adoptLister { return (&ResourcePipeline{client: c}).listForAdopt }},
	{"input", "/v1/org-1/inputs", "inputs", true, false,
		func(c *client.Client) adoptLister { return (&ResourceInput{client: c}).listForAdopt }},
	{"output", "/v1/org-1/outputs", "outputs", true, false,
		func(c *client.Client) adoptLister { return (&ResourceOutput{client: c}).listForAdopt }},
	{"enrichment", "/v3/org-1/enrichments", "enrichments", true, false,
		func(c *client.Client) adoptLister { return (&ResourceEnrichment{client: c}).listForAdopt }},
	{"transform", "/v1/org-1/transforms", "transforms", false, false,
		func(c *client.Client) adoptLister { return (&ResourceTransform{client: c}).listForAdopt }},
	{"alert rule", "/v3/org-1/alert_rules", "alert_rules", true, false,
		func(c *client.Client) adoptLister { return (&ResourceAlertRule{client: c}).listForAdopt }},
	{"secret", "/v2/org-1/secrets", "secrets", false, true,
		func(c *client.Client) adoptLister { return (&ResourceSecret{client: c}).listForAdopt }},
}

// fakeListServer serves one resource type's list endpoint from a slice the
// test mutates, so the adopt-after-timeout path can be driven
// deterministically.
type fakeListServer struct {
	srv   *httptest.Server
	items atomic.Value // []map[string]any
	calls atomic.Int32
}

func newFakeListServer(t *testing.T, fam adoptFamily) *fakeListServer {
	f := &fakeListServer{}
	f.items.Store([]map[string]any{})
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, fam.path) {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		f.calls.Add(1)
		items := f.items.Load().([]map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			fam.key:      items,
			"pagination": map[string]any{"total": len(items), "limit": 100, "offset": 0},
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeListServer) set(items ...map[string]any) { f.items.Store(items) }

// The SDK always speaks HTTPS, so the fake is a TLS server and the client
// skips certificate verification (the same use_insecure path practitioners
// use for self-signed on-prem instances).
func (f *fakeListServer) client() *client.Client {
	return client.NewMonadAPIClient(f.srv.URL, "tok", "org-1", true, 5*time.Second)
}

func (fam adoptFamily) target(c *client.Client, name string, started time.Time) adoptTarget {
	t := adoptTarget{Kind: fam.kind, Name: name, StartedAt: started, AnyAge: fam.anyAge, List: fam.lister(c)}
	if fam.typed {
		t.Type = "wanted-type"
	}
	return t
}

func item(id, name string, created time.Time) map[string]any {
	return map[string]any{
		"id": id, "name": name, "type": "wanted-type",
		"created_at": created.UTC().Format(time.RFC3339Nano),
	}
}

func shortenAdoptTimers(t *testing.T, grace, poll time.Duration) {
	oldGrace, oldPoll := adoptTimeoutGrace, adoptPollInterval
	adoptTimeoutGrace, adoptPollInterval = grace, poll
	t.Cleanup(func() { adoptTimeoutGrace, adoptPollInterval = oldGrace, oldPoll })
}

func TestAdoptAfterTimeout_AdoptsUniqueMatch(t *testing.T) {
	for _, fam := range adoptFamilies {
		t.Run(fam.kind, func(t *testing.T) {
			f := newFakeListServer(t, fam)
			started := time.Now()
			other := item("other", "Other", started.Add(time.Second))
			items := []map[string]any{item("new-1", "Ingest", started.Add(time.Second)), other}
			if !fam.anyAge {
				// Same name but created before the request: not this create's.
				items = append(items, item("old-1", "Ingest", started.Add(-time.Hour)))
			}
			if fam.typed {
				// Same name, right age, different type: not this create's.
				wrong := item("wrong-type", "Ingest", started.Add(time.Second))
				wrong["type"] = "other-type"
				items = append(items, wrong)
			}
			f.set(items...)
			got, err := adoptAfterTimeout(context.Background(), fam.target(f.client(), "Ingest", started))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "new-1" {
				t.Fatalf("adopted %q, want new-1", got)
			}
		})
	}
}

// A create that upserts by name (monad_secret) adopts the existing object even
// though it predates the request.
func TestAdoptAfterTimeout_AnyAgeAdoptsPreexisting(t *testing.T) {
	fam := adoptFamilies[len(adoptFamilies)-1]
	if !fam.anyAge {
		t.Fatalf("expected the last family (%s) to be AnyAge", fam.kind)
	}
	f := newFakeListServer(t, fam)
	started := time.Now()
	f.set(item("sec-1", "api-key", started.Add(-24*time.Hour)))
	got, err := adoptAfterTimeout(context.Background(), fam.target(f.client(), "api-key", started))
	if err != nil || got != "sec-1" {
		t.Fatalf("got (%q, %v), want (sec-1, nil)", got, err)
	}
}

func TestAdoptAfterTimeout_PollsUntilItAppears(t *testing.T) {
	shortenAdoptTimers(t, 3*time.Second, 50*time.Millisecond)
	for _, fam := range adoptFamilies {
		t.Run(fam.kind, func(t *testing.T) {
			f := newFakeListServer(t, fam)
			started := time.Now()
			go func() {
				time.Sleep(200 * time.Millisecond)
				f.set(item("late-1", "Late", started.Add(time.Second)))
			}()
			got, err := adoptAfterTimeout(context.Background(), fam.target(f.client(), "Late", started))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "late-1" {
				t.Fatalf("adopted %q, want late-1", got)
			}
			if f.calls.Load() < 2 {
				t.Fatalf("expected at least two list polls, got %d", f.calls.Load())
			}
		})
	}
}

func TestAdoptAfterTimeout_NoMatchIsRetryable(t *testing.T) {
	shortenAdoptTimers(t, 150*time.Millisecond, 20*time.Millisecond)
	for _, fam := range adoptFamilies {
		t.Run(fam.kind, func(t *testing.T) {
			f := newFakeListServer(t, fam)
			f.set(item("x", "Something Else", time.Now()))
			_, err := adoptAfterTimeout(context.Background(), fam.target(f.client(), "Missing", time.Now()))
			if err == nil || !strings.Contains(err.Error(), "was not created") {
				t.Fatalf("expected a 'not created' error, got %v", err)
			}
		})
	}
}

func TestAdoptAfterTimeout_AmbiguousRefuses(t *testing.T) {
	for _, fam := range adoptFamilies {
		t.Run(fam.kind, func(t *testing.T) {
			f := newFakeListServer(t, fam)
			started := time.Now()
			f.set(
				item("dup-1", "Ingest", started.Add(time.Second)),
				item("dup-2", "Ingest", started.Add(2*time.Second)),
			)
			_, err := adoptAfterTimeout(context.Background(), fam.target(f.client(), "Ingest", started))
			if err == nil {
				t.Fatal("expected an ambiguity error")
			}
			for _, want := range []string{"2 " + fam.kind + "s", "dup-1", "dup-2", "terraform import"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q should mention %q", err.Error(), want)
				}
			}
		})
	}
}

func TestAdoptAfterTimeout_ListErrorSurfaces(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := client.NewMonadAPIClient(srv.URL, "tok", "org-1", true, 5*time.Second)
	for _, fam := range adoptFamilies {
		t.Run(fam.kind, func(t *testing.T) {
			_, err := adoptAfterTimeout(context.Background(), fam.target(c, "Ingest", time.Now()))
			if err == nil || !strings.Contains(err.Error(), "terraform import") || !strings.Contains(err.Error(), "boom") {
				t.Fatalf("expected an error carrying the response and telling the user to check/import, got %v", err)
			}
		})
	}
}

func TestListAdoptCandidates_IncludesUnparseableCreatedAt(t *testing.T) {
	fam := adoptFamilies[0]
	f := newFakeListServer(t, fam)
	f.set(map[string]any{"id": "weird", "name": "Ingest", "created_at": "not-a-time"})
	ids, err := listAdoptCandidates(context.Background(), fam.target(f.client(), "Ingest", time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "weird" {
		t.Fatalf("got %v, want [weird]", ids)
	}
}

// listAdoptCandidates walks every page, and stops at a short page or the
// reported total.
func TestListAdoptCandidates_Pages(t *testing.T) {
	const total = 230
	var calls int
	list := func(_ context.Context, limit, offset int32) ([]adoptCandidate, *int32, *http.Response, error) {
		calls++
		var out []adoptCandidate
		for i := offset; i < offset+limit && i < total; i++ {
			id, name := fmt.Sprintf("id-%d", i), "other"
			if i == total-1 {
				name = "Last"
			}
			out = append(out, adoptCandidate{ID: &id, Name: &name})
		}
		n := int32(total)
		return out, &n, nil, nil
	}
	ids, err := listAdoptCandidates(context.Background(), adoptTarget{Kind: "pipeline", Name: "Last", StartedAt: time.Now(), List: list})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 || ids[0] != fmt.Sprintf("id-%d", total-1) {
		t.Fatalf("got %v, want the item on the last page", ids)
	}
	if calls != 3 {
		t.Fatalf("expected 3 page requests, got %d", calls)
	}
}

func TestCreateOrAdopt(t *testing.T) {
	timeoutErr := &url.Error{Op: "Post", URL: "https://x", Err: fakeNetErr{timeout: true}}
	fam := adoptFamilies[1] // input
	started := time.Now()

	t.Run("success passes the created id through", func(t *testing.T) {
		var diags diag.Diagnostics
		id, ok := createOrAdopt(context.Background(), &diags, adoptTarget{Kind: "input"}, "created-1", nil, nil)
		if !ok || id != "created-1" || diags.HasError() || diags.WarningsCount() != 0 {
			t.Fatalf("got (%q, %v, %v), want (created-1, true, no diags)", id, ok, diags)
		}
	})

	t.Run("non-timeout error is a client error and does not list", func(t *testing.T) {
		var listed bool
		tgt := adoptTarget{Kind: "input", Name: "x", List: func(context.Context, int32, int32) ([]adoptCandidate, *int32, *http.Response, error) {
			listed = true
			return nil, nil, nil, nil
		}}
		var diags diag.Diagnostics
		_, ok := createOrAdopt(context.Background(), &diags, tgt, "", errors.New("400 Bad Request"), nil)
		if ok || !diags.HasError() || diags.Errors()[0].Summary() != "Client Error" {
			t.Fatalf("expected a Client Error diagnostic, got ok=%v diags=%v", ok, diags)
		}
		if !strings.Contains(diags.Errors()[0].Detail(), "Unable to create input") {
			t.Fatalf("detail should name the resource: %q", diags.Errors()[0].Detail())
		}
		if listed {
			t.Fatal("a non-timeout error must not trigger adoption")
		}
	})

	t.Run("timeout then adopt warns and returns the adopted id", func(t *testing.T) {
		f := newFakeListServer(t, fam)
		f.set(item("adopted-1", "Ingest", started.Add(time.Second)))
		var diags diag.Diagnostics
		id, ok := createOrAdopt(context.Background(), &diags, fam.target(f.client(), "Ingest", started), "", timeoutErr, nil)
		if !ok || id != "adopted-1" || diags.HasError() {
			t.Fatalf("got (%q, %v, %v), want (adopted-1, true, no errors)", id, ok, diags)
		}
		if diags.WarningsCount() != 1 || !strings.Contains(diags.Warnings()[0].Summary(), "Input create timed out but completed") {
			t.Fatalf("expected one adoption warning, got %v", diags)
		}
	})

	t.Run("timeout with ambiguous candidates is a timed-out error", func(t *testing.T) {
		f := newFakeListServer(t, fam)
		f.set(item("a", "Ingest", started.Add(time.Second)), item("b", "Ingest", started.Add(time.Second)))
		var diags diag.Diagnostics
		_, ok := createOrAdopt(context.Background(), &diags, fam.target(f.client(), "Ingest", started), "", timeoutErr, nil)
		if ok || !diags.HasError() || diags.Errors()[0].Summary() != "Input create timed out" {
			t.Fatalf("expected an 'Input create timed out' error, got ok=%v diags=%v", ok, diags)
		}
	})
}
