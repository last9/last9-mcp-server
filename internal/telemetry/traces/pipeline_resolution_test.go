package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTracePipelineNameResolution(t *testing.T) {
	for _, tt := range []struct{ name, op, listing, want string }{
		{"typo", "$eq", `[{"metric":{"service_name":"checkout"}}]`, "closest"},
		{"ambiguous", "$eq", `[{"metric":{"service_name":"checkout"}},{"metric":{"service_name":"checkouts"}}]`, "ambiguous"},
		{"missing", "$eq", `[]`, "missing"},
		{"regex", "$regex", `[]`, "unchanged"},
		{"negative", "$neq", `[]`, "unchanged"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			discoveries, queries := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				raw, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/prom_query_instant" {
					discoveries++
					if !strings.Contains(string(raw), "trace_endpoint_count") || !strings.Contains(string(raw), "[600s]") || !strings.Contains(string(raw), "prod") {
						t.Errorf("inventory scope=%s", raw)
					}
					_, _ = io.WriteString(w, tt.listing)
					return
				}
				queries++
				rows := `[]`
				if strings.Contains(string(raw), `"checkout"`) {
					rows = `[{"ServiceName":"checkout","Timestamp":1761550000123456789}]`
				}
				_, _ = io.WriteString(w, `{"status":"success","data":{"result":`+rows+`}}`)
			}))
			defer srv.Close()
			cfg := testChunkTracesConfig(srv.URL)
			cfg.PrometheusReadURL = "https://selected.example"
			pipeline := []map[string]any{{"type": "filter", "query": map[string]any{"$and": []any{map[string]any{tt.op: []any{"ServiceName", "checkuot"}}, map[string]any{"$eq": []any{"resources['deployment.environment']", "prod"}}}}}}
			result, _, err := NewGetTracesHandler(srv.Client(), cfg)(context.Background(), nil, GetTracesArgs{TracejsonQuery: pipeline, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z", Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("tool error=%v", result)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			mu.Lock()
			defer mu.Unlock()
			switch tt.want {
			case "closest":
				if discoveries != 1 || queries != 2 || len(result.Content) < 2 || !strings.Contains(text, "1761550000123456789") || !strings.Contains(result.Content[1].(*mcp.TextContent).Text, "closest match") {
					t.Fatalf("calls=%d/%d payload=%s", discoveries, queries, text)
				}
			case "unchanged":
				if discoveries != 0 || queries != 1 {
					t.Fatalf("unsafe discovery: %d/%d", discoveries, queries)
				}
			default:
				var got map[string]any
				if err := json.Unmarshal([]byte(text), &got); err != nil {
					t.Fatal(err)
				}
				if got["status"] != tt.want || discoveries != 1 || queries != 1 {
					t.Fatalf("refusal=%s calls=%d/%d", text, discoveries, queries)
				}
			}
		})
	}
}
