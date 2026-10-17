package alerting

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

func alertsMonitorTestConfig(url string) models.Config {
	cfg := models.Config{APIBaseURL: url}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "mock-token", ExpiresAt: time.Now().Add(time.Hour)}
	return cfg
}

func TestFetchAlertsMonitorSendsWindowAndDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/alerts/monitor") {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("timestamp") != "1700003600" || r.URL.Query().Get("window") != "1800" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(AlertsResponse{Timestamp: 1700003600, Window: 1800, AlertRules: []AlertRuleData{{RuleID: "r1", Since: 1700002000}}})
	}))
	defer server.Close()

	got, err := FetchAlertsMonitor(context.Background(), server.Client(), alertsMonitorTestConfig(server.URL), AlertsMonitorQuery{Timestamp: 1700003600, Window: 1800})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AlertRules) != 1 || got.AlertRules[0].RuleID != "r1" || got.AlertRules[0].Since != 1700002000 {
		t.Fatalf("decoded = %+v", got)
	}
}

func TestFetchAlertsMonitorNamesUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := FetchAlertsMonitor(context.Background(), server.Client(), alertsMonitorTestConfig(server.URL), AlertsMonitorQuery{Timestamp: 1700003600, Window: 60})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want status 503 named", err)
	}
}
