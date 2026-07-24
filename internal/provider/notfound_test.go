package provider

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsNotFoundResponse covers the not-found detection that lets a resource
// deleted outside Terraform self-heal (ENG-9259). It must recognize a proper
// 404/410 as well as the current Monad API sentinel — HTTP 500 with body
// "An item of this type does not exist." (ENG-9258) — while leaving genuine
// server errors as hard failures.
func TestIsNotFoundResponse(t *testing.T) {
	sentinel := []byte(`{"code":500,"error":"An item of this type does not exist."}`)

	cases := []struct {
		name string
		resp *http.Response
		body []byte
		want bool
	}{
		{"404 status", &http.Response{StatusCode: http.StatusNotFound}, nil, true},
		{"410 status", &http.Response{StatusCode: http.StatusGone}, nil, true},
		{"500 with sentinel body", &http.Response{StatusCode: http.StatusInternalServerError}, sentinel, true},
		{"sentinel body, nil response", nil, sentinel, true},
		{"500 with unrelated body", &http.Response{StatusCode: http.StatusInternalServerError}, []byte(`{"code":500,"error":"boom"}`), false},
		{"400 with unrelated body", &http.Response{StatusCode: http.StatusBadRequest}, []byte("bad request"), false},
		{"nil response, nil body", nil, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isNotFoundResponse(tc.resp, tc.body))
		})
	}
}
