package client

import (
	"context"
	"io"
	"net/http"
	"time"
)

// transport adds the API key to every request and, for requests whose context
// carries no deadline, bounds them with the provider-level request_timeout.
// Requests that already have a deadline (a resource's `timeouts {}` block)
// are left alone, so a per-resource timeout can exceed the provider default.
type transport struct {
	apiToken       string
	defaultTimeout time.Duration
	next           http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "ApiKey "+t.apiToken)

	if _, hasDeadline := req.Context().Deadline(); hasDeadline || t.defaultTimeout <= 0 {
		return t.next.RoundTrip(req)
	}

	ctx, cancel := context.WithTimeout(req.Context(), t.defaultTimeout)
	resp, err := t.next.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	// The deadline must also cover reading the body, and the context must stay
	// alive until the caller is done with it -- cancel when the body is closed.
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
