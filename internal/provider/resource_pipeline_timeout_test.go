package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// fakePipelinesServer serves GET /api/v2/{org}/pipelines from a list that the
// test mutates, so the adopt-after-timeout path can be driven deterministically.
type fakePipelinesServer struct {
	srv       *httptest.Server
	pipelines atomic.Value // []map[string]any
	calls     atomic.Int32
}

func newFakePipelinesServer(t *testing.T) *fakePipelinesServer {
	f := &fakePipelinesServer{}
	f.pipelines.Store([]map[string]any{})
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/pipelines") {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		f.calls.Add(1)
		items := f.pipelines.Load().([]map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pipelines":  items,
			"pagination": map[string]any{"total": len(items), "limit": 100, "offset": 0},
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePipelinesServer) set(items ...map[string]any) { f.pipelines.Store(items) }

func newTestPipelineResource(f *fakePipelinesServer) *ResourcePipeline {
	// The SDK always speaks HTTPS, so the fake is a TLS server and the client
	// skips certificate verification (the same use_insecure path practitioners
	// use for self-signed on-prem instances).
	c := client.NewMonadAPIClient(f.srv.URL, "tok", "org-1", true, 5*time.Second)
	return &ResourcePipeline{client: c}
}

func pipelineJSON(id, name string, created time.Time) map[string]any {
	return map[string]any{"id": id, "name": name, "created_at": created.UTC().Format(time.RFC3339Nano)}
}

func TestAdoptPipelineAfterTimeout_AdoptsUniqueMatch(t *testing.T) {
	f := newFakePipelinesServer(t)
	r := newTestPipelineResource(f)
	started := time.Now()
	f.set(
		pipelineJSON("old-1", "Ingest", started.Add(-time.Hour)), // pre-existing same name: excluded by created_at
		pipelineJSON("new-1", "Ingest", started.Add(time.Second)),
		pipelineJSON("other", "Other", started.Add(time.Second)),
	)
	got, err := r.adoptPipelineAfterTimeout(context.Background(), "Ingest", started)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "new-1" {
		t.Fatalf("adopted %q, want new-1", got)
	}
}

func TestAdoptPipelineAfterTimeout_PollsUntilItAppears(t *testing.T) {
	f := newFakePipelinesServer(t)
	r := newTestPipelineResource(f)
	oldGrace, oldPoll := adoptTimeoutGrace, adoptPollInterval
	adoptTimeoutGrace, adoptPollInterval = 3*time.Second, 50*time.Millisecond
	t.Cleanup(func() { adoptTimeoutGrace, adoptPollInterval = oldGrace, oldPoll })

	started := time.Now()
	go func() {
		time.Sleep(200 * time.Millisecond)
		f.set(pipelineJSON("late-1", "Late", started.Add(time.Second)))
	}()
	got, err := r.adoptPipelineAfterTimeout(context.Background(), "Late", started)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "late-1" {
		t.Fatalf("adopted %q, want late-1", got)
	}
	if f.calls.Load() < 2 {
		t.Fatalf("expected at least two list polls, got %d", f.calls.Load())
	}
}

func TestAdoptPipelineAfterTimeout_NoMatchIsRetryable(t *testing.T) {
	f := newFakePipelinesServer(t)
	r := newTestPipelineResource(f)
	oldGrace, oldPoll := adoptTimeoutGrace, adoptPollInterval
	adoptTimeoutGrace, adoptPollInterval = 150*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { adoptTimeoutGrace, adoptPollInterval = oldGrace, oldPoll })

	f.set(pipelineJSON("x", "Something Else", time.Now()))
	_, err := r.adoptPipelineAfterTimeout(context.Background(), "Missing", time.Now())
	if err == nil || !strings.Contains(err.Error(), "was not created") {
		t.Fatalf("expected a 'not created' error, got %v", err)
	}
}

func TestAdoptPipelineAfterTimeout_AmbiguousRefuses(t *testing.T) {
	f := newFakePipelinesServer(t)
	r := newTestPipelineResource(f)
	started := time.Now()
	f.set(
		pipelineJSON("dup-1", "Ingest", started.Add(time.Second)),
		pipelineJSON("dup-2", "Ingest", started.Add(2*time.Second)),
	)
	_, err := r.adoptPipelineAfterTimeout(context.Background(), "Ingest", started)
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"2 pipelines", "dup-1", "dup-2", "terraform import"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err.Error(), want)
		}
	}
}

func TestListPipelinesByName_IncludesUnparseableCreatedAt(t *testing.T) {
	f := newFakePipelinesServer(t)
	r := newTestPipelineResource(f)
	f.set(map[string]any{"id": "weird", "name": "Ingest", "created_at": "not-a-time"})
	ids, err := r.listPipelinesByName(context.Background(), "Ingest", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "weird" {
		t.Fatalf("got %v, want [weird]", ids)
	}
}

func TestAdoptPipelineAfterTimeout_ListErrorSurfaces(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := client.NewMonadAPIClient(srv.URL, "tok", "org-1", true, 5*time.Second)
	r := &ResourcePipeline{client: c}
	_, err := r.adoptPipelineAfterTimeout(context.Background(), "Ingest", time.Now())
	if err == nil || !strings.Contains(err.Error(), "terraform import") {
		t.Fatalf("expected an error telling the user to check/import, got %v", err)
	}
}
