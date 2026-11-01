package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func TestLabelLookupNameResolution(t *testing.T) {
	for _, tt := range []struct {
		name, metric, label, labels, want string
		listNames, failure                bool
	}{
		{"metric in labels", "checkuot_requests", "", `["service_name"]`, "metric", true, false},
		{"metric in values", "checkuot_requests", "service_name", `["service_name"]`, "metric", false, false},
		{"label typo", "checkout_requests", "servcie_name", `["service_name"]`, "label", false, false},
		{"label ambiguous", "checkout_requests", "servcie_name", `["service_name","service_names"]`, "ambiguous", false, false},
		{"label missing", "checkout_requests", "unknown_label", `["service_name"]`, "missing", false, false},
		{"label failure", "checkout_requests", "servcie_name", `[]`, "", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["read_url"] != "https://selected.example" || body["window"] != float64(600) || body["timestamp"] != float64(1700000600) {
					t.Errorf("scope=%v", body)
				}
				if r.URL.Path == "/apm/labels" {
					if body["metric"] != `checkout_requests{env="prod"}` {
						_, _ = io.WriteString(w, `{"labels":[]}`)
						return
					}
					if tt.failure {
						w.WriteHeader(503)
					}
					_, _ = io.WriteString(w, `{"labels":`+tt.labels+`}`)
					return
				}
				if body["label"] == "__name__" {
					if body["matches"].([]any)[0] != `{env="prod"}` {
						t.Errorf("metric discovery scope=%v", body)
					}
					_, _ = io.WriteString(w, `["checkout_requests"]`)
					return
				}
				if body["label"] == "service_name" && body["matches"].([]any)[0] == `checkout_requests{env="prod"}` {
					_, _ = io.WriteString(w, `["checkout"]`)
				} else {
					_, _ = io.WriteString(w, `[]`)
				}
			}))
			defer srv.Close()
			cfg := models.Config{APIBaseURL: srv.URL, PrometheusReadURL: "https://selected.example", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
			query := tt.metric + `{env="prod"}`
			if tt.listNames {
				result, _, err := NewPromqlLabelsHandler(srv.Client(), cfg)(context.Background(), nil, PromqlLabelsArgs{MatchQuery: query, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
				if err != nil {
					t.Fatal(err)
				}
				texts := resultTexts(result)
				if len(texts) != 2 || texts[0] != `{"labels":["service_name"]}` || !strings.Contains(texts[1], "closest match") {
					t.Fatalf("result=%v", texts)
				}
				return
			}
			result, _, err := NewPromqlLabelValuesHandler(srv.Client(), cfg)(context.Background(), nil, PromqlLabelValuesArgs{MatchQuery: query, Label: tt.label, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
			if tt.failure {
				if err == nil || !strings.Contains(err.Error(), "discovery") {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			texts := resultTexts(result)
			if tt.want == "metric" || tt.want == "label" {
				if len(texts) != 2 || texts[0] != `["checkout"]` || !strings.Contains(texts[1], "Using "+tt.want) || !strings.Contains(texts[1], "closest match") {
					t.Fatalf("result=%v", texts)
				}
			} else {
				var got map[string]any
				if err := json.Unmarshal([]byte(texts[0]), &got); err != nil {
					t.Fatal(err)
				}
				if result.IsError || got["entity"] != "label" || got["status"] != tt.want {
					t.Fatalf("refusal=%v", got)
				}
			}
		})
	}
}
