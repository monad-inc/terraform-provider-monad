package client

import (
	"net/http"
)

var _ http.RoundTripper = &transport{}

type transport struct {
	apiToken string
	next     http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "ApiKey "+t.apiToken)

	return t.next.RoundTrip(req)
}
