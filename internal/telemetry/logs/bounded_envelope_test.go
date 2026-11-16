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
		for _, count := range []int{2, 6, 40} {
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

				// Clients join text blocks, so the whole result must stay one JSON document.
				var joined strings.Builder
				for _, content := range result.Content {
					joined.WriteString(content.(*mcp.TextContent).Text)
				}
				var payload struct {
					Envelope struct {
						TrimmedItems int      `json:"trimmed_items"`
						Hints        []string `json:"hints"`
					} `json:"l9_envelope"`
				}
				if err := json.Unmarshal([]byte(joined.String()), &payload); err != nil {
					t.Fatalf("joined text is not one JSON document: %v", err)
				}
				hints := strings.Join(payload.Envelope.Hints, "\n")
				if !strings.Contains(hints, "scan_budget_exceeded") {
					t.Errorf("text client lost guardrail hint: %q", hints)
				}

				// The 32 KB budget bounds the text a client reads once; 6 entries of 4 KB fit.
				fits := count*4000 < maxRawResultBytes
				if fits && payload.Envelope.TrimmedItems != 0 {
					t.Errorf("trimmed %d of %d entries that fit the budget", payload.Envelope.TrimmedItems, count)
				}
				if !fits {
					if payload.Envelope.TrimmedItems == 0 {
						t.Error("over-budget response not trimmed")
					}
					if want := fmt.Sprintf("trimmed to fit %d bytes", maxRawResultBytes); !strings.Contains(hints, want) {
						t.Errorf("trim hint does not name the raw budget %q: %q", want, hints)
					}
				}
			})
		}
	}
}

func TestServiceLogsGuardrailHintsDoNotReduceRawEntries(t *testing.T) {
	values := make([]logValue, 500)
	for i := range values {
		values[i] = logValue{Timestamp: "60000000000", Message: strings.Repeat("x", 100)}
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

	args := GetServiceLogsArgs{ServiceName: "api", LookbackMinutes: 5, Limit: 500, ResponseMode: "raw"}
	off, _, err := NewGetServiceLogsHandler(srv.Client(), testLogsConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	shadowCfg := testLogsConfig(srv.URL)
	shadowCfg.QueryGuardrail = guardrailtest.Limits(guardrail.ModeShadow)
	shadow, _, err := NewGetServiceLogsHandler(srv.Client(), shadowCfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}

	offCount := decodeServiceLogs(t, off).Count
	shadowCount := decodeServiceLogs(t, shadow).Count
	if shadowCount != offCount {
		t.Fatalf("shadow returned %d entries, want the off-mode baseline %d", shadowCount, offCount)
	}
}
