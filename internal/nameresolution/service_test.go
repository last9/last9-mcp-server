package nameresolution

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api/apitest"
	"last9-mcp/internal/models"
)

func TestResolveService(t *testing.T)         { testResolveService(t, false) }
func TestResolveServiceInMemory(t *testing.T) { testResolveService(t, true) }
func testResolveService(t *testing.T, memory bool) {
	for _, tt := range []struct {
		name, requested, listing, status string
		code                             int
	}{
		{"exact", "checkout", `[{"metric":{"service_name":"checkout"}}]`, "exact", 200},
		{"typo", "checkuot", `[{"metric":{"service_name":"checkout"}}]`, "closest", 200},
		{"ambiguous", "checkuot", `[{"metric":{"service_name":"checkout"}},{"metric":{"service_name":"checkouts"}}]`, "ambiguous", 200},
		{"missing", "payments", `[]`, "missing", 200},
		{"wrong entity", "checkout-eu", `[{"metric":{"service_name":"checkout-us"}}]`, "missing", 200},
		{"sibling", "checkout", `[{"metric":{"service_name":"checkout-prod"}}]`, "ambiguous", 200},
		{"failure", "checkuot", `[]`, "", 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []string{"logs", "traces"} {
				t.Run(kind, func(t *testing.T) {
					base, client := apitest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var got struct {
							Query, ReadURL string
							Timestamp      int64
						}
						var fields map[string]any
						if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
							t.Error(err)
						}
						got.Query, _ = fields["query"].(string)
						if fields["read_url"] != "https://selected.example" || fields["timestamp"] != float64(1700003600) {
							t.Errorf("scope=%v", fields)
						}
						for _, want := range []string{`env="prod"`, "service_name"} {
							if !strings.Contains(got.Query, want) {
								t.Errorf("query=%s; missing %s", got.Query, want)
							}
						}
						if kind == "logs" && !strings.Contains(got.Query, `name="archive"`) {
							t.Errorf("index missing: %s", got.Query)
						}
						if kind == "traces" && !strings.Contains(got.Query, "[3600s]") {
							t.Errorf("window missing: %s", got.Query)
						}
						w.WriteHeader(tt.code)
						_, _ = io.WriteString(w, tt.listing)
					}), memory)
					cfg := models.Config{APIBaseURL: base, PrometheusReadURL: "https://selected.example", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
					in := ServiceLookup{Requested: tt.requested, Telemetry: kind, Start: 1700000000, End: 1700003600, Matchers: []string{`env="prod"`}}
					if kind == "logs" {
						in.Index = "physical_index:archive"
					}
					got, err := ResolveService(context.Background(), client, cfg, in)
					if tt.code != 200 {
						if err == nil || !strings.Contains(err.Error(), "discovery") {
							t.Fatalf("error=%v", err)
						}
						return
					}
					if err != nil || got.Status != tt.status {
						t.Fatalf("resolution=%+v err=%v", got, err)
					}
				})
			}
		})
	}
}
