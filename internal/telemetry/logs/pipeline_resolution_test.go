package logs

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

func TestLogsPipelineNameResolution(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		resolve     bool
	}{
		{"equality", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","checkuot"]},{"$eq":["resources['deployment.environment']","prod"]},{"$eq":["Body","checkuot"]}]}}]`, true},
		{"regex", `[{"type":"filter","query":{"$regex":["ServiceName","checkuot"]}}]`, false},
		{"negative", `[{"type":"filter","query":{"$neq":["ServiceName","checkuot"]}}]`, false},
		{"body literal", `[{"type":"filter","query":{"$eq":["Body","checkuot"]}}]`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			discoveries := 0
			var queries []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				raw, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/prom_query_instant" {
					discoveries++
					if !strings.Contains(string(raw), "prod") {
						t.Errorf("missing env: %s", raw)
					}
					_, _ = io.WriteString(w, `[{"metric":{"service_name":"checkout"}}]`)
					return
				}
				queries = append(queries, string(raw))
				entries := `[]`
				if strings.Contains(string(raw), `"checkout"`) {
					entries = `[{"stream":{},"values":[["1700000000000000000","found"]]}]`
				}
				_, _ = io.WriteString(w, `{"query_result":{"status":"success","data":{"resultType":"streams","result":`+entries+`}},"total_matching_lines":0}`)
			}))
			defer srv.Close()
			cfg := apiModeCfg(srv.URL)
			cfg.PrometheusReadURL = "https://selected.example"
			var pipeline []map[string]any
			if err := json.Unmarshal([]byte(tt.query), &pipeline); err != nil {
				t.Fatal(err)
			}
			result, _, err := NewGetLogsHandler(srv.Client(), cfg)(context.Background(), nil, GetLogsArgs{LogjsonQuery: pipeline, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z", Limit: 5, ResponseMode: "raw"})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if tt.resolve {
				if discoveries != 1 || len(queries) != 2 || len(result.Content) < 2 {
					t.Fatalf("discovery=%d queries=%d content=%d", discoveries, len(queries), len(result.Content))
				}
				if !strings.Contains(queries[1], `"Body","checkuot"`) || !strings.Contains(result.Content[1].(*mcp.TextContent).Text, "closest match") {
					t.Fatalf("literal or disclosure changed: %v", queries)
				}
			} else if discoveries != 0 || len(queries) != 1 {
				t.Fatalf("unsafe resolution: discovery=%d queries=%d", discoveries, len(queries))
			}
		})
	}
}
