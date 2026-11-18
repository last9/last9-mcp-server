package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var wideParsePipeline = []map[string]interface{}{
	{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{
		map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}},
	}}},
	{"type": "parse", "parser": "logfmt", "field": "Body"},
	{"type": "aggregate", "aggregates": []interface{}{
		map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "n"},
	}, "groupby": map[string]interface{}{"attributes['route']": "route"}},
}

func statusLogServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"internal"}`, status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hangingLogServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv
}

func assertScanGuidance(t *testing.T, result *mcp.CallToolResult, err error, wantCause string, wantSteps ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("handler returned a bare error, want a result with l9_guidance: %v", err)
	}
	if !result.IsError {
		t.Fatal("result must be an error result")
	}
	if result.Meta[utils.ErrorTypeMetaKey] != utils.SystemErrorType {
		t.Errorf("meta = %v, want the system_error tag kept", result.Meta)
	}
	payload := parseToolJSONResult(t, result)
	if msg, _ := payload["error"].(string); msg == "" {
		t.Errorf("payload %v must keep the upstream error text", payload)
	}
	guidance, ok := payload["l9_guidance"].(map[string]any)
	if !ok {
		t.Fatalf("payload %v has no l9_guidance", payload)
	}
	if guidance["cause"] != wantCause {
		t.Errorf("cause = %v, want %s", guidance["cause"], wantCause)
	}
	text := guidanceText(guidance)
	for _, want := range append(wantSteps, "unchanged") {
		if !strings.Contains(text, want) {
			t.Errorf("guidance %q missing %q", text, want)
		}
	}
}

func TestGetLogsHandler_ScanFailureCarriesGuidance(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := statusLogServer(t, status)
			result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery: wideParsePipeline,
				StartTimeISO: "2026-10-09T00:00:00Z",
				EndTimeISO:   "2026-10-16T00:00:00Z",
			})
			assertScanGuidance(t, result, err, "scan_failed", "`$contains`", "before", "`parse`", "shorten the window")
		})
	}
}

func TestGetLogsHandler_ScanTimeoutCarriesGuidance(t *testing.T) {
	srv := hangingLogServer(t)
	cfg := apiModeCfg(srv.URL)
	cfg.LogSearchTimeout = 50 * time.Millisecond

	result, _, err := NewGetLogsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: wideParsePipeline,
		StartTimeISO: "2026-10-09T00:00:00Z",
		EndTimeISO:   "2026-10-16T00:00:00Z",
	})
	assertScanGuidance(t, result, err, "timeout", "`$contains`", "`parse`", "shorten the window")
}

func TestGetLogsHandler_GatewayTimeoutCarriesTimeoutGuidance(t *testing.T) {
	srv := statusLogServer(t, http.StatusGatewayTimeout)
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: wideParsePipeline, LookbackMinutes: 60,
	})
	assertScanGuidance(t, result, err, "timeout", "shorten the window")
}

func TestGetLogsHandler_BadRequestStaysAPlainError(t *testing.T) {
	srv := statusLogServer(t, http.StatusBadRequest)
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: wideParsePipeline, LookbackMinutes: 60,
	})
	if err == nil || result != nil {
		t.Fatalf("a 400 is the caller's to fix and keeps its plain error; got result=%v err=%v", result, err)
	}
}

func TestGetLogPatterns_ScanFailureCarriesGuidance(t *testing.T) {
	srv := statusLogServer(t, http.StatusInternalServerError)
	result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", LookbackMinutes: 7 * 24 * 60, SampleSize: 5000},
	})
	assertScanGuidance(t, result, err, "scan_failed", "`body_filters`", "shorten the window", "`sample_size`")
}

func TestGetLogPatterns_ScanTimeoutCarriesGuidance(t *testing.T) {
	srv := statusLogServer(t, http.StatusGatewayTimeout)
	result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", LookbackMinutes: 7 * 24 * 60, SampleSize: 5000},
	})
	assertScanGuidance(t, result, err, "timeout", "`body_filters`", "shorten the window", "`sample_size`")
}
