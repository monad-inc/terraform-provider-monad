package client

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"

	monad "github.com/monad-inc/sdk/go"
)

// DefaultRequestTimeout bounds every HTTP request the provider makes when the
// practitioner does not set `request_timeout`. Pipeline creation in particular
// can take well over a minute when several pipelines are created concurrently
// (the API serializes them), and the old fixed 60 s budget made Terraform
// record such creates as failed while the server went on to finish them
// (ENG-10257).
const DefaultRequestTimeout = 5 * time.Minute

type Client struct {
	*monad.APIClient

	OrganizationID string
	// RequestTimeout is the per-request budget the HTTP client enforces.
	RequestTimeout time.Duration
}

func NewMonadAPIClient(host, apiToken, organizationID string, isInsecure bool, requestTimeout time.Duration) *Client {
	if requestTimeout <= 0 {
		requestTimeout = DefaultRequestTimeout
	}

	debugEnvvar := os.Getenv("DEBUG")

	var debug bool
	if debugEnvvar == "true" {
		debug = true
	}

	return &Client{
		OrganizationID: organizationID,
		RequestTimeout: requestTimeout,
		APIClient: monad.NewAPIClient(&monad.Configuration{
			Debug:     debug,
			UserAgent: "terraform-provider-monad/1.0",
			Scheme:    "https",
			Servers: []monad.ServerConfiguration{
				{
					URL: host + "/api",
				},
			},
			HTTPClient: &http.Client{
				Timeout: requestTimeout,
				Transport: &transport{
					apiToken: apiToken,
					next: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: isInsecure,
						},
					},
				},
			},
		}),
	}
}
