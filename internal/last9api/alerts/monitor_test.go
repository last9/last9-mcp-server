package alerts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestGetMonitorRequestAndResponse(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got = r
		_, _ = io.WriteString(w, `{"alert_rules":[{"rule_id":"r1","state":"firing","alerts":[{"state":"firing"},{"state":"resolved"}]}]}`)
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		Region:       "ap-south-1",
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}

	resp, err := GetMonitor(context.Background(), last9api.NewClient(srv.Client(), cfg), GetMonitorInput{Timestamp: 1700000000, Window: 900})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.AlertRules) != 1 || resp.AlertRules[0].RuleID != "r1" || len(resp.AlertRules[0].Alerts) != 2 || resp.AlertRules[0].Alerts[1].State != "resolved" {
		t.Errorf("alert rules = %+v", resp.AlertRules)
	}
	q := got.URL.Query()
	if got.Method != http.MethodGet || got.URL.Path != "/alerts/monitor" || q.Get("timestamp") != "1700000000" || q.Get("window") != "900" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
}
