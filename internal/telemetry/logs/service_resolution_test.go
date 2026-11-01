package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/last9api/apitest"
)

func TestServiceLogsNameResolution(t *testing.T)         { testServiceLogsNameResolution(t, false) }
func TestServiceLogsNameResolutionInMemory(t *testing.T) { testServiceLogsNameResolution(t, true) }
func testServiceLogsNameResolution(t *testing.T, memory bool) {
	for _, tt := range []struct {
		name, requested, listing, want string
		failure                        bool
	}{
		{"exact", "checkout", `[]`, "checkout", false},
		{"typo", "checkuot", `[{"metric":{"service_name":"checkout"}}]`, "checkout", false},
		{"ambiguous", "checkuot", `[{"metric":{"service_name":"checkout"}},{"metric":{"service_name":"checkouts"}}]`, "ambiguous", false},
		{"missing", "unknown", `[]`, "missing", false},
		{"wrong entity", "checkout-eu", `[{"metric":{"service_name":"checkout-us"}}]`, "missing", false},
		{"failed discovery", "checkuot", `[]`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var searches, discoveries int
			base, client := apitest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				body, _ := io.ReadAll(r.Body)
				switch r.URL.Path {
				case "/prom_query_instant":
					discoveries++
					if !strings.Contains(string(body), "physical_index_service_count") || !strings.Contains(string(body), "prod") {
						t.Errorf("inventory scope=%s", body)
					}
					if tt.failure {
						w.WriteHeader(503)
					}
					_, _ = io.WriteString(w, tt.listing)
				case "/logs/query":
					searches++
					var got map[string]any
					if err := json.Unmarshal(body, &got); err != nil {
						t.Error(err)
					}
					if got["start"] != float64(1700000000) || got["end"] != float64(1700000600) || got["limit"] != float64(5) {
						t.Errorf("search scope=%s", body)
					}
					entries := `[]`
					if strings.Contains(string(body), `"checkout"`) {
						entries = `[{"stream":{},"values":[["1700000000000000000","found"]]}]`
					}
					_, _ = io.WriteString(w, `{"query_result":{"status":"success","data":{"resultType":"streams","result":`+entries+`}},"total_matching_lines":0}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}), memory)
			cfg := apiModeCfg(base)
			cfg.PrometheusReadURL = "https://selected.example"
			result, _, err := NewGetServiceLogsHandler(client, cfg)(context.Background(), nil, GetServiceLogsArgs{ServiceName: tt.requested, Env: "prod", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z", Limit: 5, ResponseMode: "raw"})
			if tt.failure {
				if err == nil || !strings.Contains(err.Error(), "discovery") {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("unexpected tool error: %v", result)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
				t.Fatal(err)
			}
			if tt.want == "checkout" {
				if got["service"] != "checkout" || got["count"] != float64(1) {
					t.Fatalf("payload=%v", got)
				}
				if tt.name == "typo" && (len(result.Content) < 2 || !strings.Contains(result.Content[1].(*mcp.TextContent).Text, `closest match to "checkuot"`)) {
					t.Fatalf("missing disclosure: %v", result.Content)
				}
			} else if got["status"] != tt.want || got["logs"] != nil {
				t.Fatalf("refusal=%v", got)
			}
			mu.Lock()
			defer mu.Unlock()
			if tt.name == "exact" && (searches != 1 || discoveries != 0) {
				t.Fatalf("exact calls: %d %d", searches, discoveries)
			}
			if tt.name != "exact" && discoveries != 1 {
				t.Fatalf("discoveries=%d", discoveries)
			}
			if tt.want != "checkout" && searches != 1 {
				t.Fatalf("refusal executed %d searches", searches)
			}
		})
	}
}
