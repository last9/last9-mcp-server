package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAPMDeviationsDatasourceResolution(t *testing.T) {
	testAPMDeviationsDatasourceResolution(t, false)
}

func TestAPMDeviationsDatasourceResolutionInMemory(t *testing.T) {
	testAPMDeviationsDatasourceResolution(t, true)
}

func testAPMDeviationsDatasourceResolution(t *testing.T, inMemory bool) {
	for _, tt := range []struct {
		name, requested, selected, note, candidates, wantErr string
	}{
		{name: "exact", requested: "staging", selected: "staging"},
		{name: "typo", requested: "stagign", selected: "staging", note: `Using datasource "staging" (closest match to "stagign").`},
		{name: "followups", requested: "stagign", selected: "staging", note: `Using datasource "staging" (closest match to "stagign").`},
		{name: "invalid args", requested: "stagign", wantErr: "max_services must be between 1 and 10"},
		{name: "case", requested: "STAGING", selected: "staging", note: `Using datasource "staging" (closest match to "STAGING").`},
		{name: "default", selected: "prod-us", note: `Using datasource "prod-us" (default).`},
		{name: "uncached default", selected: "prod-us", note: `Using datasource "prod-us" (default).`},
		{name: "blank", requested: "  ", selected: "prod-us", note: `Using datasource "prod-us" (default).`},
		{name: "ambiguous", requested: "prod", candidates: "prod-eu, prod-us"},
		{name: "distant", requested: "stagingxx", candidates: "staging"},
		{name: "region", requested: "prod-ap", wantErr: `datasource "prod-ap" not found; available: prod-eu, prod-us, staging`},
		{name: "missing", requested: "billing", wantErr: `datasource "billing" not found; available: prod-eu, prod-us, staging`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct {
					Timestamp int64  `json:"timestamp"`
					ReadURL   string `json:"read_url"`
					Username  string `json:"username"`
					Password  string `json:"password"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if tt.selected == "" || body.ReadURL != "https://"+tt.selected+".example.com/prom" || body.Username != tt.selected+"-user" || body.Password != tt.selected+"-password" {
					t.Errorf("query used wrong datasource: %+v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				if tt.name == "followups" && body.Timestamp < 1783760400 {
					_, _ = w.Write([]byte(`[]`))
					return
				}
				_, _ = w.Write([]byte(`[{"metric":{"service_name":"api","env":"prod"},"value":[1783760400,"10"]}]`))
			})
			apiURL, client := deviationDatasourceClient(t, handler, inMemory)
			cfg := models.Config{
				APIBaseURL:         apiURL,
				PrometheusReadURL:  "https://prod-us.example.com/prom",
				PrometheusUsername: "prod-us-user", PrometheusPassword: "prod-us-password",
				Region:       "us-east-1",
				TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
			}
			for _, name := range []string{"prod-us", "prod-eu", "staging"} {
				cfg.Datasources = append(cfg.Datasources, models.DatasourceInfo{
					Name: name, ReadURL: "https://" + name + ".example.com/prom", Username: name + "-user", Password: name + "-password",
					Region: "us-east-1", ClusterID: name + "-cluster", IsDefault: name == "prod-us",
				})
			}
			if tt.name == "uncached default" {
				cfg.Datasources = nil
				cfg.DatasourceName = "prod-us"
			}
			args := DeviationArgs{Datasource: tt.requested, StartTimeISO: "2026-07-11T08:00:00Z", EndTimeISO: "2026-07-11T09:00:00Z"}
			if tt.name == "invalid args" {
				args.MaxServices = -1
			}
			res, _, err := NewAPMServiceDeviationsHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, args)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("error = %v, want %q", err, tt.wantErr)
				}
				if calls.Load() != 0 {
					t.Errorf("rejected request made %d queries", calls.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("unexpected tool error: %v", resultTexts(res))
			}
			texts := resultTexts(res)
			if tt.candidates != "" {
				want := fmt.Sprintf("datasource %q is not an exact name. No query was run; retry with one of these as datasource: %s.", tt.requested, tt.candidates)
				if calls.Load() != 0 || len(texts) != 1 || texts[0] != want || res.StructuredContent != nil {
					t.Errorf("candidate result = %+v, queries = %d; want only %q", res, calls.Load(), want)
				}
				return
			}
			var payload apmDeviationResult
			if err := json.Unmarshal([]byte(texts[0]), &payload); err != nil {
				t.Fatal(err)
			}
			structured, ok := res.StructuredContent.(apmDeviationResult)
			wantServices := 1
			if tt.name == "followups" {
				wantServices = 0
				if len(payload.TelemetryChanges) != 1 || len(payload.RecommendedFollowups) != 1 || payload.RecommendedFollowups[0].Arguments["datasource"] != tt.selected {
					t.Errorf("follow-ups lost canonical datasource: %+v", payload.RecommendedFollowups)
				}
			}
			if calls.Load() != 32 || len(payload.Services) != wantServices || payload.Datasource != tt.selected || !ok || structured.Datasource != tt.selected {
				t.Errorf("queries = %d, services = %d, datasource = %q, structured datasource = %q", calls.Load(), len(payload.Services), payload.Datasource, structured.Datasource)
			}
			if note := strings.Join(texts[1:], "\n"); note != tt.note {
				t.Errorf("note = %q, want %q", note, tt.note)
			}
			if tt.requested == "stagign" {
				_, err := QueryServiceDeviations(context.Background(), client, cfg, args)
				if err == nil || !strings.Contains(err.Error(), "not found") || calls.Load() != 32 {
					t.Errorf("composite caller must retain exact lookup: err=%v queries=%d", err, calls.Load())
				}
			}
		})
	}
}

func deviationDatasourceClient(t *testing.T, handler http.Handler, inMemory bool) (string, *http.Client) {
	t.Helper()
	if inMemory {
		return "https://api.example.com", &http.Client{Transport: apmRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			return recorder.Result(), nil
		})}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL, server.Client()
}
