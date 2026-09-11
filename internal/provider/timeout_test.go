package provider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

type fakeNetErr struct{ timeout bool }

func (e fakeNetErr) Error() string   { return "fake net error" }
func (e fakeNetErr) Timeout() bool   { return e.timeout }
func (e fakeNetErr) Temporary() bool { return false }

func TestIsTimeoutError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"wrapped deadline", errors.Join(errors.New("post failed"), context.DeadlineExceeded), true},
		{"url.Error timeout", &url.Error{Op: "Post", URL: "https://x", Err: fakeNetErr{timeout: true}}, true},
		{"url.Error non-timeout", &url.Error{Op: "Post", URL: "https://x", Err: fakeNetErr{timeout: false}}, false},
		{"net.Error timeout", net.Error(fakeNetErr{timeout: true}), true},
		{"client timeout message", errors.New(`Post "https://x": net/http: request canceled (Client.Timeout exceeded while awaiting headers)`), true},
		{"plain error", errors.New("boom"), false},
		{"context canceled", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTimeoutError(tc.err); got != tc.want {
				t.Fatalf("isTimeoutError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestParseRequestTimeout(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", client.DefaultRequestTimeout, false},
		{"5m", 5 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"1h30m", 90 * time.Minute, false},
		{"0", 0, true},
		{"-1m", 0, true},
		{"five minutes", 0, true},
		{"300", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseRequestTimeout(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseRequestTimeout(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRequestTimeout(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parseRequestTimeout(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNewMonadAPIClientTimeout(t *testing.T) {
	c := client.NewMonadAPIClient("https://example.invalid", "tok", "org", false, 0)
	if c.RequestTimeout != client.DefaultRequestTimeout {
		t.Fatalf("zero timeout should fall back to default %v, got %v", client.DefaultRequestTimeout, c.RequestTimeout)
	}
	if got := c.GetConfig().HTTPClient.Timeout; got != 0 {
		t.Fatalf("http.Client.Timeout should be unset (deadlines come from contexts), got %v", got)
	}
	c = client.NewMonadAPIClient("https://example.invalid", "tok", "org", false, 42*time.Second)
	if c.RequestTimeout != 42*time.Second {
		t.Fatalf("RequestTimeout = %v, want 42s", c.RequestTimeout)
	}
}

// TestTransportAppliesDefaultTimeoutOnlyWithoutDeadline: a request with no
// context deadline is bounded by request_timeout; one that carries its own
// (longer) deadline is not cut short by the provider default.
func TestTransportAppliesDefaultTimeoutOnlyWithoutDeadline(t *testing.T) {
	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pipelines":[],"pagination":{"total":0}}`))
	}))
	t.Cleanup(slow.Close)
	c := client.NewMonadAPIClient(slow.URL, "tok", "org-1", true, 100*time.Millisecond)

	// No deadline on the context -> the 100 ms provider default applies.
	_, _, err := c.PipelinesAPI.ListPipelines(context.Background(), "org-1").Execute()
	if !isTimeoutError(err) {
		t.Fatalf("expected a timeout from the provider default, got %v", err)
	}

	// A longer per-operation deadline wins over the provider default.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := c.PipelinesAPI.ListPipelines(ctx, "org-1").Execute(); err != nil {
		t.Fatalf("per-operation deadline should not be cut short by the default: %v", err)
	}
}

// Every resource exposes a `timeouts` block with create/read/update/delete.
func TestEveryResourceHasTimeoutsBlock(t *testing.T) {
	for _, mk := range New("test")().Resources(context.Background()) {
		r := mk()
		var meta resource.MetadataResponse
		r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "monad"}, &meta)
		var sr resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
		if sr.Diagnostics.HasError() {
			t.Fatalf("%s: schema diagnostics: %v", meta.TypeName, sr.Diagnostics)
		}
		blk, ok := sr.Schema.Blocks["timeouts"]
		if !ok {
			t.Fatalf("%s: no timeouts block", meta.TypeName)
		}
		attrs := blk.GetNestedObject().GetAttributes()
		for _, want := range []string{"create", "read", "update", "delete"} {
			if _, ok := attrs[want]; !ok {
				t.Fatalf("%s: timeouts block lacks %q", meta.TypeName, want)
			}
		}
	}
}
