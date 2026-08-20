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
		Body:       io.NopCloser(strings.NewReader(`{"error":"parse error: unexpected identifier","url":"https://internal.example/v1?token=SECRET"}`)),
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
	if !strings.Contains(got, "[redacted-url]") {
		t.Fatalf("expected URL redaction, got %s", got)
	}
	if strings.Contains(got, "https://") || strings.Contains(got, "SECRET") {
		t.Fatalf("400 body leaked internals: %s", got)
	}
}

func TestNewUpstreamHTTPErrorTruncatesAndHints(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"` + strings.Repeat("x", 800) + `"}`)),
	}
	err := NewUpstreamHTTPError(resp, "logs query", LogsPipelineSchemaHint)
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.Contains(got, "… (truncated)") {
		t.Fatalf("expected truncation marker, got %s", got)
	}
	if !strings.Contains(got, "get_log_attributes_for_pipeline") {
		t.Fatalf("expected pipeline hint, got %s", got)
	}
}

func TestNewUpstreamHTTPErrorMapsTooManySamplesToActionableMetricsError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Body: io.NopCloser(strings.NewReader(
			`{"error":"Too many samples queried. Please try selecting a smaller time range."}`,
		)),
	}
	err := NewUpstreamHTTPError(resp, "Prometheus range query")
	if err == nil {
		t.Fatal("expected error")
	}

	got := err.Error()
	for _, want := range []string{
		"HTTP 422",
		"METRICS_QUERY_TOO_MANY_SAMPLES",
		"narrower filters",
		"service",
		"environment",
		"operation",
		"label",
		"do not ask the user to edit PromQL",
		"split it into smaller subranges",
		"preserve the requested coverage",
		"Never average percentile values",
		"Ask the user to narrow the scope only when",
		"Do not retry the same query unchanged",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("custom metrics error %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "Upstream response") {
		t.Fatalf("custom metrics error should replace the raw upstream response: %s", got)
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
