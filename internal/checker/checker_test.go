package checker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNonRetryableHTTPError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "HTTP 404 Not Found",
			err:      errors.New("checker: GET \"https://example.com\" HTTP 404"),
			expected: true,
		},
		{
			name:     "HTTP 403 Forbidden",
			err:      errors.New("checker: GET \"https://example.com\" HTTP 403"),
			expected: true,
		},
		{
			name:     "HTTP 400 Bad Request",
			err:      errors.New("checker: GET \"https://example.com\" HTTP 400"),
			expected: true,
		},
		{
			name:     "HTTP 401 Unauthorized",
			err:      errors.New("checker: GET \"https://example.com\" HTTP 401"),
			expected: true,
		},
		{
			name:     "HTTP 500 Internal Server Error (retryable)",
			err:      errors.New("checker: GET \"https://example.com\" HTTP 500"),
			expected: false,
		},
		{
			name:     "HTTP 502 Bad Gateway (retryable)",
			err:      errors.New("checker: GET \"https://example.com\" HTTP 502"),
			expected: false,
		},
		{
			name:     "Network connection reset (retryable)",
			err:      errors.New("read: connection reset by peer"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isNonRetryableHTTPError(tc.err)
			if got != tc.expected {
				t.Errorf("isNonRetryableHTTPError(%v) = %v; want %v", tc.err, got, tc.expected)
			}
		})
	}
}

func TestFetchJSONAndHTMLNonRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notfound":
			http.Error(w, "custom 404", http.StatusNotFound)
		case "/forbidden":
			http.Error(w, "custom 403", http.StatusForbidden)
		case "/servererror":
			http.Error(w, "custom 500", http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"key": "value"}`))
		}
	}))
	defer server.Close()

	ctx := context.Background()

	// Test fetchJSON 404
	var dest map[string]string
	err404 := fetchJSON(ctx, server.URL+"/notfound", &dest)
	if err404 == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !isNonRetryableHTTPError(err404) {
		t.Fatalf("expected error %q to be non-retryable", err404)
	}

	// Test fetchHTML 403
	_, err403 := fetchHTML(ctx, server.URL+"/forbidden")
	if err403 == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !isNonRetryableHTTPError(err403) {
		t.Fatalf("expected error %q to be non-retryable", err403)
	}

	// Test fetchJSON 500 (should be retryable)
	err500 := fetchJSON(ctx, server.URL+"/servererror", &dest)
	if err500 == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if isNonRetryableHTTPError(err500) {
		t.Fatalf("expected error %q to be retryable, got non-retryable", err500)
	}
}
