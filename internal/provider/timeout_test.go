package provider

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

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
	if got := c.GetConfig().HTTPClient.Timeout; got != client.DefaultRequestTimeout {
		t.Fatalf("http client timeout = %v, want %v", got, client.DefaultRequestTimeout)
	}
	c = client.NewMonadAPIClient("https://example.invalid", "tok", "org", false, 42*time.Second)
	if got := c.GetConfig().HTTPClient.Timeout; got != 42*time.Second {
		t.Fatalf("http client timeout = %v, want 42s", got)
	}
}
