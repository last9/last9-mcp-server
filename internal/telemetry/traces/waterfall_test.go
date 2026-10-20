package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apitraces "last9-mcp/internal/last9api/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const waterfallFixturePath = "../../../contracts/fixtures/evidence-trace-waterfall.json"

// The fixture is the trace service's golden waterfall response.
func waterfallFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(waterfallFixturePath))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// waterfallFixtureWith returns the fixture, compacted, with edit applied to its top level.
func waterfallFixtureWith(t *testing.T, edit func(map[string]any)) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(waterfallFixture(t), &payload); err != nil {
		t.Fatal(err)
	}
	edit(payload)
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type waterfallCall struct {
	result  *mcp.CallToolResult
	err     error
	request *http.Request
}

func callWaterfallHandler(t *testing.T, status int, body []byte, args GetTraceWaterfallArgs) waterfallCall {
	t.Helper()
	var call waterfallCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call.request = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	call.result, _, call.err = handler(context.Background(), &mcp.CallToolRequest{}, args)
	return call
}

func waterfallText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("want one content item, got %+v", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", result.Content[0])
	}
	return text.Text
}

func TestGetTraceWaterfallRelaysBackendEvidenceVerbatim(t *testing.T) {
	fixture := waterfallFixture(t)
	call := callWaterfallHandler(t, http.StatusOK, fixture, GetTraceWaterfallArgs{
		TraceID:        testValidTraceID,
		Environment:    "production",
		SelectedSpanID: testValidSpanID,
		StartTimeISO:   "2026-07-15T08:30:00Z",
		EndTimeISO:     "2026-07-15T08:45:00Z",
		MaxSpans:       250,
	})
	if call.err != nil {
		t.Fatal(call.err)
	}
	if got, want := waterfallText(t, call.result), string(bytes.TrimSpace(fixture)); got != want {
		t.Fatalf("tool output differs from the backend body:\n%s", got)
	}
	if link, _ := call.result.Meta["reference_url"].(string); !strings.Contains(link, testValidTraceID) {
		t.Fatalf("reference_url = %q, want a link to the trace", link)
	}

	r := call.request
	if r.Method != http.MethodGet || r.URL.Path != "/cat/api/traces/v2/waterfall/"+testValidTraceID {
		t.Fatalf("request = %s %s", r.Method, r.URL)
	}
	q := r.URL.Query()
	for key, want := range map[string]string{
		"start": "1784104200", "end": "1784105100", "max_spans": "250",
		"selected_span_id": testValidSpanID, "environment": "production", "region": "ap-south-1",
	} {
		if q.Get(key) != want {
			t.Errorf("query %s = %q, want %q", key, q.Get(key), want)
		}
	}
}

func TestGetTraceWaterfallDefaultsMaxSpans(t *testing.T) {
	call := callWaterfallHandler(t, http.StatusOK, waterfallFixture(t), GetTraceWaterfallArgs{TraceID: testValidTraceID})
	if call.err != nil {
		t.Fatal(call.err)
	}
	if got := call.request.URL.Query().Get("max_spans"); got != "500" {
		t.Fatalf("max_spans = %q, want the 500 default", got)
	}
}

func TestGetTraceWaterfallRejectsOutOfRangeMaxSpansLocally(t *testing.T) {
	for _, maxSpans := range []int{-1, 1001} {
		server, n := countingTraceDetailsServer(t)
		handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
		_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{TraceID: testValidTraceID, MaxSpans: maxSpans})
		if err == nil || !strings.Contains(err.Error(), "max_spans must be between 1 and 1000") {
			t.Fatalf("max_spans=%d: got %v", maxSpans, err)
		}
		if n.Load() != 0 {
			t.Fatalf("max_spans=%d made %d upstream requests, want 0", maxSpans, n.Load())
		}
	}
}

func assertWaterfallRefused(t *testing.T, err error, reason waterfallRefusal) {
	t.Helper()
	var refused *waterfallRefusedError
	if !errors.As(err, &refused) || refused.Reason != reason {
		t.Fatalf("want a %q refusal, got %#v (%v)", reason, err, err)
	}
}

func TestGetTraceWaterfallFailsClosedOnSanitizerBlock(t *testing.T) {
	cases := map[string]struct {
		edit   func(map[string]any)
		reason waterfallRefusal
	}{
		"missing":         {func(p map[string]any) { delete(p, "sanitizer") }, refusalSanitizerMissing},
		"null":            {func(p map[string]any) { p["sanitizer"] = nil }, refusalSanitizerMissing},
		"not an object":   {func(p map[string]any) { p["sanitizer"] = "trace-evidence-sanitizer/v1" }, refusalSanitizerMissing},
		"version absent":  {func(p map[string]any) { delete(p["sanitizer"].(map[string]any), "version") }, refusalSanitizerVersion},
		"version blank":   {func(p map[string]any) { p["sanitizer"].(map[string]any)["version"] = "" }, refusalSanitizerVersion},
		"version v2":      {func(p map[string]any) { p["sanitizer"].(map[string]any)["version"] = "trace-evidence-sanitizer/v2" }, refusalSanitizerVersion},
		"version upper":   {func(p map[string]any) { p["sanitizer"].(map[string]any)["version"] = "TRACE-EVIDENCE-SANITIZER/V1" }, refusalSanitizerVersion},
		"version padded":  {func(p map[string]any) { p["sanitizer"].(map[string]any)["version"] = " trace-evidence-sanitizer/v1" }, refusalSanitizerVersion},
		"version numeric": {func(p map[string]any) { p["sanitizer"].(map[string]any)["version"] = 1 }, refusalSanitizerVersion},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			call := callWaterfallHandler(t, http.StatusOK, waterfallFixtureWith(t, tc.edit), GetTraceWaterfallArgs{TraceID: testValidTraceID})
			if call.result != nil {
				t.Fatalf("unsanitized evidence reached the model: %s", waterfallText(t, call.result))
			}
			assertWaterfallRefused(t, call.err, tc.reason)
		})
	}
}

// Go's decoder matches keys case-insensitively and keeps the last duplicate; the
// check must read the exact key a client reads.
func TestGetTraceWaterfallReadsTheExactSanitizerKey(t *testing.T) {
	body := waterfallFixtureWith(t, func(p map[string]any) {
		p["Sanitizer"] = p["sanitizer"]
		delete(p, "sanitizer")
	})
	call := callWaterfallHandler(t, http.StatusOK, body, GetTraceWaterfallArgs{TraceID: testValidTraceID})
	assertWaterfallRefused(t, call.err, refusalSanitizerMissing)
}

func TestGetTraceWaterfallRejectsNonConformingEvidence(t *testing.T) {
	cases := map[string][]byte{
		"empty body":             nil,
		"not an object":          []byte(`[]`),
		"wrong contract version": waterfallFixtureWith(t, func(p map[string]any) { p["contract_version"] = "investigation-evidence/v2" }),
		"wrong analysis version": waterfallFixtureWith(t, func(p map[string]any) { p["analysis_version"] = "trace-attribute-deviations/v1" }),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			call := callWaterfallHandler(t, http.StatusOK, body, GetTraceWaterfallArgs{TraceID: testValidTraceID})
			if call.err == nil {
				t.Fatalf("non-conforming evidence was relayed: %s", waterfallText(t, call.result))
			}
			if !strings.Contains(call.err.Error(), "invalid response") {
				t.Fatalf("got %v", call.err)
			}
		})
	}
}

// paddedWaterfall returns a valid fixture body of exactly size bytes.
func paddedWaterfall(t *testing.T, size int) []byte {
	t.Helper()
	base := waterfallFixtureWith(t, func(p map[string]any) { p["padding"] = "" })
	pad := size - len(base)
	if pad < 0 {
		t.Fatalf("fixture is already %d bytes", len(base))
	}
	return waterfallFixtureWith(t, func(p map[string]any) { p["padding"] = strings.Repeat("a", pad) })
}

func TestGetTraceWaterfallOutputCap(t *testing.T) {
	t.Run("at cap", func(t *testing.T) {
		body := paddedWaterfall(t, apitraces.MaxWaterfallBytes)
		call := callWaterfallHandler(t, http.StatusOK, body, GetTraceWaterfallArgs{TraceID: testValidTraceID})
		if call.err != nil {
			t.Fatal(call.err)
		}
		if got := waterfallText(t, call.result); len(got) != apitraces.MaxWaterfallBytes {
			t.Fatalf("relayed %d bytes, want %d", len(got), apitraces.MaxWaterfallBytes)
		}
	})
	t.Run("cap+1", func(t *testing.T) {
		body := paddedWaterfall(t, apitraces.MaxWaterfallBytes+1)
		call := callWaterfallHandler(t, http.StatusOK, body, GetTraceWaterfallArgs{TraceID: testValidTraceID})
		assertWaterfallRefused(t, call.err, refusalTooLarge)
		if !strings.Contains(call.err.Error(), "max_spans") {
			t.Fatalf("an oversized waterfall must say how to shrink it: %v", call.err)
		}
	})
}

// The route is only registered once the trace service enables it, so a 404 is a
// missing capability, not a missing trace.
func TestGetTraceWaterfallRouteNotFoundIsNotTraceNotFound(t *testing.T) {
	call := callWaterfallHandler(t, http.StatusNotFound, []byte(`404 page not found`), GetTraceWaterfallArgs{TraceID: testValidTraceID})
	if call.err == nil || strings.Contains(call.err.Error(), "No trace found") || !strings.Contains(call.err.Error(), "unavailable or disabled") {
		t.Fatalf("got %v", call.err)
	}
}

func TestGetTraceWaterfallTransportErrorHidesTheURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{TraceID: testValidTraceID})
	if err == nil || !strings.Contains(err.Error(), "could not be reached") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("got %v", err)
	}
}

func TestGetTraceWaterfallRelaysSanitizedValidationErrors(t *testing.T) {
	body := []byte(`{"code":"evidence_too_large","detail":"lower max_spans; token=s3cr3t"}`)
	call := callWaterfallHandler(t, http.StatusUnprocessableEntity, body, GetTraceWaterfallArgs{TraceID: testValidTraceID})
	if call.err == nil || !strings.Contains(call.err.Error(), "evidence_too_large") || strings.Contains(call.err.Error(), "s3cr3t") {
		t.Fatalf("got %v", call.err)
	}
}
