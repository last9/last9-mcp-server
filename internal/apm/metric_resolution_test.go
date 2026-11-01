package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func TestMetricQueryNameResolution(t *testing.T) {
	for _, tool := range []string{"instant", "range", "status"} {
		for _, tt := range []struct {
			name, query, listing, want string
			failure                    bool
		}{
			{"exact", `checkout_requests{env="prod"}`, `[]`, "data", false},
			{"typo", `checkuot_requests{env="prod"}`, `["checkout_requests"]`, "closest", false},
			{"ambiguous", `checkuot_requests{env="prod"}`, `["checkout_requests","checkouts_requests"]`, "ambiguous", false},
			{"missing", `missing_metric{env="prod"}`, `[]`, "missing", false},
			{"wrong entity", `checkout_eu_requests{env="prod"}`, `["checkout_us_requests"]`, "missing", false},
			{"discovery failure", `checkuot_requests{env="prod"}`, `[]`, "", true},
			{"expression unchanged", `sum(checkuot_requests{env="prod"})`, `[]`, "unchanged", false},
		} {
			t.Run(tool+"/"+tt.name, func(t *testing.T) {
				var mu sync.Mutex
				var bodies []map[string]any
				discoveries := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					bodies = append(bodies, body)
					if body["read_url"] != "https://selected.example" {
						t.Errorf("wrong datasource: %v", body)
					}
					if r.URL.Path == "/prom_label_values" {
						discoveries++
						if body["label"] != "__name__" || body["matches"].([]any)[0] != `{env="prod"}` {
							t.Errorf("discovery scope=%v", body)
						}
						if tt.failure {
							w.WriteHeader(503)
						}
						_, _ = io.WriteString(w, tt.listing)
						return
					}
					query, _ := body["query"].(string)
					if strings.Contains(query, "checkout_requests") {
						_, _ = io.WriteString(w, `[{"metric":{"env":"prod"},"value":[1700000600,"1"]}]`)
					} else {
						_, _ = io.WriteString(w, `[]`)
					}
				}))
				defer srv.Close()
				cfg := models.Config{APIBaseURL: srv.URL, PrometheusReadURL: "https://default.example", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}, Datasources: []models.DatasourceInfo{{Name: "selected", ReadURL: "https://selected.example", Username: "selected", Password: "test", Region: "test-region"}}}
				var result *mcp.CallToolResult
				var err error
				switch tool {
				case "instant":
					result, _, err = NewPromqlInstantQueryHandler(srv.Client(), cfg)(context.Background(), nil, PromqlInstantQueryArgs{Query: tt.query, TimeISO: "2023-11-14T22:23:20Z", Datasource: "selected"})
				case "range":
					result, _, err = NewPromqlRangeQueryHandler(srv.Client(), cfg)(context.Background(), nil, PromqlRangeQueryArgs{Query: tt.query, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z", Datasource: "selected"})
				case "status":
					result, _, err = NewGetMetricStatusHandler(srv.Client(), cfg)(context.Background(), nil, GetMetricStatusArgs{Metric: tt.query, WindowMinutes: 10, Datasource: "selected"})
				}
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
					t.Fatalf("error result=%v", result)
				}
				texts := resultTexts(result)
				mu.Lock()
				defer mu.Unlock()
				if tt.want == "closest" {
					if discoveries != 1 || len(texts) < 2 || !strings.Contains(texts[1], `closest match to "checkuot_requests"`) {
						t.Fatalf("discovery=%d result=%v", discoveries, texts)
					}
					firstTimestamp := bodies[0]["timestamp"]
					for _, body := range bodies {
						if body["timestamp"] != firstTimestamp {
							t.Errorf("time changed: %v", bodies)
						}
					}
				} else if tt.want == "data" || tt.want == "unchanged" {
					if discoveries != 0 {
						t.Fatalf("unnecessary discovery=%d", discoveries)
					}
				} else {
					var refusal map[string]any
					if err := json.Unmarshal([]byte(texts[0]), &refusal); err != nil {
						t.Fatal(err)
					}
					if refusal["status"] != tt.want || len(bodies) != 2 {
						t.Fatalf("refusal=%v calls=%d", refusal, len(bodies))
					}
				}
			})
		}
	}
}

func TestGetMetricStatusNameResolutionRequiresValidCount(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		resolves   bool
	}{
		{"missing value", `[{"metric":{}}]`, false},
		{"null value", `[{"metric":{},"value":null}]`, false},
		{"short value", `[{"metric":{},"value":[1700000600]}]`, false},
		{"invalid number", `[{"metric":{},"value":[1700000600,"invalid"]}]`, false},
		{"NaN", `[{"metric":{},"value":[1700000600,"NaN"]}]`, false},
		{"positive infinity", `[{"metric":{},"value":[1700000600,"+Inf"]}]`, false},
		{"negative infinity", `[{"metric":{},"value":[1700000600,"-Inf"]}]`, false},
		{"zero with missing value", `[{"metric":{},"value":[1700000600,"0"]},{"metric":{}}]`, false},
		{"empty array", `[]`, true},
		{"zero count", `[{"metric":{},"value":[1700000600,"0"]}]`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var queries []string
			discoveries := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var body struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.URL.Path == "/prom_label_values" {
					discoveries++
					_, _ = io.WriteString(w, `["checkout_requests"]`)
					return
				}
				queries = append(queries, body.Query)
				if strings.Contains(body.Query, "checkuot_requests") {
					_, _ = io.WriteString(w, tt.body)
				} else {
					_, _ = io.WriteString(w, `[{"metric":{},"value":[1700000600,"1"]}]`)
				}
			}))
			defer srv.Close()
			cfg := models.Config{APIBaseURL: srv.URL, PrometheusReadURL: "https://metrics.example", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
			result, _, err := NewGetMetricStatusHandler(srv.Client(), cfg)(context.Background(), nil, GetMetricStatusArgs{Metric: "checkuot_requests", WindowMinutes: 10})
			mu.Lock()
			defer mu.Unlock()
			if !tt.resolves {
				if discoveries != 0 || len(queries) != 1 {
					t.Errorf("invalid count triggered fallback: discoveries=%d queries=%v", discoveries, queries)
				}
				if err == nil || !strings.Contains(err.Error(), "count") {
					t.Fatalf("err=%v, want count-response error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if discoveries != 1 || len(queries) != 4 {
				t.Fatalf("discoveries=%d queries=%v, want discovery and three canonical queries", discoveries, queries)
			}
			fields := decodeMetricStatusFields(t, result)
			if fields["live"] != true || fields["series_count"] != float64(1) || !strings.Contains(strings.Join(resultTexts(result), "\n"), `closest match to "checkuot_requests"`) {
				t.Fatalf("unexpected canonical result: %v", resultTexts(result))
			}
		})
	}
}
