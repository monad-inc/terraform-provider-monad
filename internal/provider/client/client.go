package client

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"

	monad "github.com/monad-inc/sdk/go"
)

type Client struct {
	*monad.APIClient

	OrganizationID string
}

func NewMonadAPIClient(host, apiToken, organizationID string, isInsecure bool) *Client {
	debugEnvvar := os.Getenv("DEBUG")

	var debug bool
	if debugEnvvar == "true" {
		debug = true
	}

	return &Client{
		OrganizationID: organizationID,
		APIClient: monad.NewAPIClient(&monad.Configuration{
			Debug:  debug,
			Scheme: "https",
			Servers: []monad.ServerConfiguration{
				{
					URL: host + "/api",
				},
			},
			HTTPClient: &http.Client{
				Timeout: time.Minute,
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
