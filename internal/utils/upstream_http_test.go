package utils

import (
	"errors"
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
	err := NewUpstreamHTTPErrorWithAdvice(resp, "logs query", UpstreamAdvice{SchemaHint: LogsPipelineSchemaHint})
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

func TestUnknownFieldNextActionMatchesEachMarker(t *testing.T) {
	for _, body := range []string{
		`unknown field "bad_op"`,
		`{"error":"Unknown attribute attributes['user']"}`,
		`code: 47. DB::Exception: Unknown identifier: foo`,
		`Missing columns: 'foo' while processing query`,
		`unknown column foo`,
		`no such column: foo`,
	} {
		got := UnknownFieldNextAction(body, "get_log_attributes_for_pipeline")
		if !strings.Contains(got, "get_log_attributes_for_pipeline") {
			t.Errorf("body %q: next action = %q, want it to name the discovery tool", body, got)
		}
	}
}

func TestUnknownFieldNextActionIgnoresOtherErrors(t *testing.T) {
	for _, body := range []string{`parse error: unexpected identifier`, `invalid stage type`, ``} {
		if got := UnknownFieldNextAction(body, "get_log_attributes_for_pipeline"); got != "" {
			t.Errorf("body %q: next action = %q, want none", body, got)
		}
	}
	if got := UnknownFieldNextAction(`unknown field "x"`, ""); got != "" {
		t.Errorf("no discovery tool: next action = %q, want none", got)
	}
}

func TestNewUpstreamHTTPErrorWithAdviceLeadsWithDiscoveryTool(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"unknown field attributes['usr']"}`)),
	}
	got := NewUpstreamHTTPErrorWithAdvice(resp, "logs query", UpstreamAdvice{
		SchemaHint:    "SCHEMA",
		DiscoveryTool: "get_log_attributes_for_pipeline",
	}).Error()
	next := strings.Index(got, "Next action: call get_log_attributes_for_pipeline")
	body := strings.Index(got, "Upstream response:")
	if next < 0 || body < 0 || next > body {
		t.Fatalf("want the next action before the upstream body, got %s", got)
	}
	if strings.Contains(got, "Review the tool arguments") {
		t.Fatalf("generic advice should give way to the next action: %s", got)
	}
	if !strings.HasSuffix(got, "SCHEMA") {
		t.Fatalf("schema hint missing: %s", got)
	}
}

func TestNewUpstreamHTTPErrorAuthNamesScopeAndRole(t *testing.T) {
	for status, want := range map[int][]string{
		http.StatusUnauthorized: {"HTTP 401", "rejected", "regenerate the refresh token", "API Access"},
		http.StatusForbidden:    {"HTTP 403", "scope", "role", "admin"},
	} {
		resp := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`denied`))}
		got := NewUpstreamHTTPError(resp, "logs query").Error()
		for _, fragment := range append(want, "Do not retry unchanged") {
			if !strings.Contains(got, fragment) {
				t.Errorf("HTTP %d error %q missing %q", status, got, fragment)
			}
		}
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

func TestNewUpstreamHTTPErrorIsTyped(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("unknown chart key"))}
	var typed *UpstreamHTTPError
	if !errors.As(NewUpstreamHTTPError(resp, "op"), &typed) || typed.StatusCode != http.StatusBadRequest || typed.Body != "unknown chart key" {
		t.Fatalf("unexpected typed error: %+v", typed)
	}
	resp = &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("boom"))}
	if !errors.As(NewUpstreamHTTPError(resp, "op"), &typed) || typed.StatusCode != http.StatusBadGateway || typed.Body != "" {
		t.Fatalf("unexpected typed error: %+v", typed)
	}
}

func TestNewUpstreamHTTPErrorKeeps4xxBodyOutOfMessage(t *testing.T) {
	for _, tt := range []struct {
		status    int
		wantBody  string
		inMessage bool
	}{
		{http.StatusForbidden, "Not allowed for viewer role", false},
		{http.StatusConflict, "Not allowed for viewer role", false},
		{http.StatusBadRequest, "Not allowed for viewer role", true},
		{http.StatusUnprocessableEntity, "Not allowed for viewer role", true},
		{http.StatusBadGateway, "", false},
	} {
		resp := &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader("Not allowed for viewer role"))}
		err := NewUpstreamHTTPError(resp, "op")
		var typed *UpstreamHTTPError
		if !errors.As(err, &typed) || typed.Body != tt.wantBody {
			t.Fatalf("status %d: Body = %+v", tt.status, typed)
		}
		if got := strings.Contains(err.Error(), "viewer role"); got != tt.inMessage {
			t.Fatalf("status %d: body in Error() = %v: %s", tt.status, got, err)
		}
	}
}
