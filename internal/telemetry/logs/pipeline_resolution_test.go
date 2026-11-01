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
	testLogsPipelineNameResolution(t, true)
}

func TestLogsPipelineNameResolutionChunked(t *testing.T) {
	testLogsPipelineNameResolution(t, false)
}

func testLogsPipelineNameResolution(t *testing.T, searchAPI bool) {
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
				response := `{"status":"success","data":{"resultType":"streams","result":` + entries + `}}`
				if searchAPI {
					response = `{"query_result":` + response + `,"total_matching_lines":0}`
				}
				_, _ = io.WriteString(w, response)
			}))
			defer srv.Close()
			cfg := apiModeCfg(srv.URL)
			cfg.UseLogSearchAPI = searchAPI
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

func TestLogsPipelineNameResolutionRequiresEveryChunkEmpty(t *testing.T) {
	for _, mode := range []string{"complete", "oldest partial", "null"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			discoveries, canonical, original := 0, 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				raw, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/prom_query_instant" {
					discoveries++
					_, _ = io.WriteString(w, `[{"metric":{"service_name":"checkout"}}]`)
					return
				}
				if strings.Contains(string(raw), `"checkout"`) {
					canonical++
				} else {
					original++
				}
				rows, metadata := `[]`, ``
				if mode == "oldest partial" && r.URL.Query().Get("start") == "0" {
					metadata = `,"partial":true`
				}
				if mode == "null" {
					rows = `null`
				}
				_, _ = io.WriteString(w, `{"status":"success"`+metadata+`,"data":{"resultType":"streams","result":`+rows+`}}`)
			}))
			defer srv.Close()
			cfg := apiModeCfg(srv.URL)
			cfg.UseLogSearchAPI = false
			cfg.PrometheusReadURL = "https://selected.example"
			_, _, err := NewGetLogsHandler(srv.Client(), cfg)(context.Background(), nil, GetLogsArgs{
				LogjsonQuery: []map[string]any{{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "checkuot"}}}},
				StartTimeISO: "1970-01-01T00:00:00Z", EndTimeISO: "1970-01-01T01:30:00Z",
			})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			wantDiscovery, wantCanonical := 0, 0
			if mode == "complete" {
				wantDiscovery, wantCanonical = 1, 6
			}
			if original != 6 || discoveries != wantDiscovery || canonical != wantCanonical {
				t.Fatalf("original=%d discoveries=%d canonical=%d", original, discoveries, canonical)
			}
		})
	}
}
