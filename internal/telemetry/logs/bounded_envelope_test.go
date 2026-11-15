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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/telemetry/guardrail"
	"last9-mcp/internal/telemetry/guardrail/guardrailtest"
)

func TestBoundedLogEnvelopeContract(t *testing.T) {
	for _, tool := range []string{"get_logs", "get_service_logs"} {
		for _, count := range []int{2, 40} {
			t.Run(fmt.Sprintf("%s/%d", tool, count), func(t *testing.T) {
				values := make([]logValue, count)
				for i := range values {
					values[i] = logValue{Timestamp: "60000000000", Message: strings.Repeat("x", 4000)}
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
				cfg.QueryGuardrail = guardrailtest.Limits(guardrail.ModeShadow)
				var result *mcp.CallToolResult
				var err error
				if tool == "get_logs" {
					result, _, err = NewGetLogsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
						LogjsonQuery:    []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}},
						LookbackMinutes: 5,
						Limit:           50,
						ResponseMode:    "raw",
					})
				} else {
					result, _, err = NewGetServiceLogsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
						ServiceName:     "api",
						LookbackMinutes: 5,
						Limit:           50,
						ResponseMode:    "raw",
					})
				}
				if err != nil {
					t.Fatal(err)
				}
				if result.IsError {
					t.Fatal("unexpected tool error")
				}

				encoded, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				var structured map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &structured); err != nil {
					t.Fatal(err)
				}
				if _, ok := structured["result"]; !ok {
					t.Error("structured payload missing")
				}

				foundHint := false
				for _, content := range result.Content {
					if text, ok := content.(*mcp.TextContent); ok && strings.HasPrefix(text.Text, "envelope: ") && strings.Contains(text.Text, "scan_budget_exceeded") {
						foundHint = true
					}
				}
				if !foundHint {
					t.Error("text-only client lost guardrail hint")
				}
			})
		}
	}
}
