package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callGetServiceLogs(t *testing.T, server *httptest.Server, cfg func(string) models.Config, args GetServiceLogsArgs) *mcp.CallToolResult {
	t.Helper()
	result, _, err := NewGetServiceLogsHandler(server.Client(), cfg(server.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func decodeServiceLogs(t *testing.T, result *mcp.CallToolResult) ServiceLogsResponse {
	t.Helper()
	var response ServiceLogsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &response); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return response
}

var wholeSecondWindow = GetServiceLogsArgs{
	ServiceName:  "checkout",
	StartTimeISO: "2026-01-01T00:00:00Z",
	EndTimeISO:   "2026-01-01T00:10:00Z",
	Limit:        3,
}

func TestGetServiceLogsChunkedProbesOnePastLimit(t *testing.T) {
	for _, tc := range []struct {
		matched       int
		wantTruncated bool
		wantCoverage  string
		wantKept      int
	}{
		{matched: 2, wantCoverage: "complete", wantKept: 2},
		{matched: 3, wantCoverage: "complete", wantKept: 3},
		{matched: 4, wantTruncated: true, wantCoverage: "partial", wantKept: 3},
	} {
		var limits []string
		server := streamsServer(t, nLogValues(tc.matched, "boom"), &limits)
		result := callGetServiceLogs(t, server, testLogsConfig, wholeSecondWindow)

		if len(limits) != 1 || limits[0] != "4" {
			t.Fatalf("matched %d: requested limits = %v, want one probe for limit+1", tc.matched, limits)
		}
		if got := decodeServiceLogs(t, result); got.Count != tc.wantKept || len(got.Logs) != tc.wantKept {
			t.Fatalf("matched %d: count %d logs %d, want %d", tc.matched, got.Count, len(got.Logs), tc.wantKept)
		}
		env := utils.GetResultEnvelope(t, result)
		if env.Truncated != tc.wantTruncated || (tc.wantTruncated && env.Limit != 3) {
			t.Fatalf("matched %d: envelope %+v, want truncated=%v", tc.matched, env, tc.wantTruncated)
		}
		coverage, _ := result.Meta["last9/coverage"].(map[string]any)
		if coverage["status"] != tc.wantCoverage {
			t.Fatalf("matched %d: coverage %v, want %s", tc.matched, coverage["status"], tc.wantCoverage)
		}
	}
}

func TestGetServiceLogsEmptyResultHintsServiceDiscovery(t *testing.T) {
	server := streamsServer(t, nil, nil)
	result := callGetServiceLogs(t, server, testLogsConfig, wholeSecondWindow)

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "did_you_mean") || !strings.Contains(env.Hints[0], "get_service_profile") {
		t.Fatalf("hints = %v, want one pointing at service discovery", env.Hints)
	}
}

func TestGetServiceLogsTrimsOversizedResponse(t *testing.T) {
	server := streamsServer(t, nLogValues(40, strings.Repeat("x", 4000)), nil)
	args := wholeSecondWindow
	args.Limit = 40
	result := callGetServiceLogs(t, server, testLogsConfig, args)

	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > utils.MaxResultTextBytes {
		t.Fatalf("payload text is %d bytes, over budget", len(text))
	}
	got := decodeServiceLogs(t, result)
	env := utils.GetResultEnvelope(t, result)
	if got.Count == 0 || got.Count != len(got.Logs) || env.TrimmedItems != 40-got.Count {
		t.Fatalf("count %d logs %d trimmed_items %d, want count to match kept logs and the rest trimmed", got.Count, len(got.Logs), env.TrimmedItems)
	}
	if coverage, _ := result.Meta["last9/coverage"].(map[string]any); coverage["status"] == "complete" {
		t.Fatalf("coverage %v must not claim complete after trimming", coverage)
	}
}

func TestGetServiceLogsSearchAPIProbesOnePastLimit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		returned      int
		logsTruncated bool
		wantTruncated bool
	}{
		{name: "complete", returned: 3},
		{name: "probe row came back", returned: 4, wantTruncated: true},
		{name: "server says truncated", returned: 2, logsTruncated: true, wantTruncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sentLimit any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				sentLimit = body["limit"]
				var inner map[string]any
				_ = json.Unmarshal([]byte(streamsAPIResponse(nLogValues(tc.returned, "boom"))), &inner)
				out, _ := json.Marshal(map[string]any{"query_result": inner, "logs_truncated": tc.logsTruncated})
				_, _ = w.Write(out)
			}))
			defer srv.Close()

			result := callGetServiceLogs(t, srv, apiModeCfg, wholeSecondWindow)
			if sentLimit != float64(4) {
				t.Fatalf("sent limit = %v, want limit+1", sentLimit)
			}
			if got := decodeServiceLogs(t, result); got.Count != min(tc.returned, 3) {
				t.Fatalf("count = %d, want %d", got.Count, min(tc.returned, 3))
			}
			if env := utils.GetResultEnvelope(t, result); env.Truncated != tc.wantTruncated {
				t.Fatalf("envelope = %+v, want truncated=%v", env, tc.wantTruncated)
			}
		})
	}
}
