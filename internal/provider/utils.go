package provider

import (
	"io"
	"net/http"
)

func ptr[T any](v T) *T {
	return &v
}

func getResponseBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body
}
