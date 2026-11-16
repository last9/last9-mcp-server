package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/guardrail"
	"last9-mcp/internal/telemetry/guardrail/guardrailtest"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRawEntryCapacityWithGuardrailMetadata(t *testing.T) {
	for _, tool := range []string{"get_logs", "get_service_logs"} {
		for _, mode := range []string{guardrail.ModeOff, guardrail.ModeShadow} {
			for _, tc := range []struct {
				count int
				width int
				want  int
			}{
				{count: 2, width: 100, want: 2},
				{count: 1, width: 30600, want: 1},
				{count: 2, width: 30600, want: 1},
			} {
				name := fmt.Sprintf("%s/%s/count_%d_width_%d", tool, mode, tc.count, tc.width)
				t.Run(name, func(t *testing.T) {
					values := make([]logValue, tc.count)
					for i := range values {
						values[i] = logValue{Timestamp: "60000000000", Message: strings.Repeat("x", tc.width)}
					}
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_, _ = io.Copy(io.Discard, r.Body)
						if strings.HasSuffix(r.URL.Path, "/estimate") {
							_, _ = w.Write([]byte(`{"data":{"bytes":5001,"scoped":true}}`))
							return
						}
						_, _ = w.Write([]byte(strings.Replace(streamsAPIResponse(values), "{", `{"status":"success",`, 1)))
					}))
					defer srv.Close()

					cfg := testLogsConfig(srv.URL)
					cfg.QueryGuardrail = guardrailtest.Limits(mode)
					result := callRawCapacityTool(t, tool, srv, cfg)
					if result.IsError {
						t.Fatalf("expected %d fitting entries, got tool error: %s", tc.want, result.Content[0].(*mcp.TextContent).Text)
					}
					if got := rawCapacityCount(t, tool, result); got != tc.want {
						t.Fatalf("returned %d entries, want %d", got, tc.want)
					}
				})
			}
		}
	}
}

func callRawCapacityTool(t *testing.T, tool string, srv *httptest.Server, cfg models.Config) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	request := &mcp.CallToolRequest{}
	if tool == "get_logs" {
		result, _, err := NewGetLogsHandler(srv.Client(), cfg)(ctx, request, GetLogsArgs{
			LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}},
			StartTimeISO: "1970-01-01T00:00:00Z", EndTimeISO: "1970-01-01T00:05:00Z", Limit: 500, ResponseMode: "raw",
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result, _, err := NewGetServiceLogsHandler(srv.Client(), cfg)(ctx, request, GetServiceLogsArgs{
		ServiceName: "api", StartTimeISO: "1970-01-01T00:00:00Z", EndTimeISO: "1970-01-01T00:05:00Z", Limit: 500, ResponseMode: "raw",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func rawCapacityCount(t *testing.T, tool string, result *mcp.CallToolResult) int {
	t.Helper()
	if tool == "get_service_logs" {
		return decodeServiceLogs(t, result).Count
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
		t.Fatal(err)
	}
	_, items, err := extractResultItems(payload)
	if err != nil {
		t.Fatal(err)
	}
	return countLogEntriesInResultItems(items)
}
