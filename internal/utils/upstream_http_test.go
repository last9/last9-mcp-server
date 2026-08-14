package utils

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewUpstreamHTTPErrorRelays400Body(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"parse error: unexpected identifier"}`)),
	}
	err := NewUpstreamHTTPError(resp, "Prometheus range query")
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.Contains(got, "parse error: unexpected identifier") {
		t.Fatalf("400 body missing from error: %s", got)
	}
	if !strings.Contains(got, "HTTP 400") {
		t.Fatalf("status missing from error: %s", got)
	}
}

func TestNewUpstreamHTTPErrorDrains502Body(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader(`{"error":"gateway SECRET https://internal.example/x"}`)),
	}
	err := NewUpstreamHTTPError(resp, "logs query")
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if strings.Contains(got, "SECRET") || strings.Contains(got, "https://") {
		t.Fatalf("502 body leaked: %s", got)
	}
	if !strings.Contains(got, "HTTP 502") {
		t.Fatalf("status missing from error: %s", got)
	}
}

func TestSanitizeUpstreamBodyRedactsAndBounds(t *testing.T) {
	raw := `{"error":"bad pipeline","upstream_url":"https://internal-gw.example.com/v1/traces?token=SECRET_abc123","authorization":"Bearer eyJhbGciOi"}`
	got := SanitizeUpstreamBody(raw)
	for _, banned := range []string{"SECRET_abc123", "https://", "eyJhbGciOi"} {
		if strings.Contains(got, banned) {
			t.Fatalf("SanitizeUpstreamBody leaked %q: %s", banned, got)
		}
	}
	if !strings.Contains(got, "bad pipeline") {
		t.Fatalf("SanitizeUpstreamBody dropped the actionable rejection text: %s", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf8: %q", got)
	}
}
