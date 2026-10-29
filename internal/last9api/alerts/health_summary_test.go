package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

func newAlertsTestClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}
	return last9api.NewClient(srv.Client(), cfg)
}

func TestGetHealthSummaryRequestAndResponse(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"schema_version":2,"alerts_raised":{"status":"partial","count":7,"reason":"lifecycle data starts 2026-10-01"},"acknowledged":{"status":"unavailable","reason":"no ack data"},"delivered":{"total":{"status":"available","count":3}},"coverage":{"lifecycle":{"from":"2026-10-01"}}}`)
	})

	resp, err := GetHealthSummary(context.Background(), c, GetHealthSummaryInput{
		From: 1700000000, To: 1700086400, Timezone: "Asia/Kolkata", Severities: []string{"P1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/alerts/health_summary" {
		t.Errorf("request = %s %s", method, path)
	}
	if body["from"] != float64(1700000000) || body["to"] != float64(1700086400) || body["timezone"] != "Asia/Kolkata" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["services"]; ok {
		t.Errorf("empty services sent: %v", body)
	}
	if resp.AlertsRaised.Status != "partial" || *resp.AlertsRaised.Count != 7 || resp.AlertsRaised.Reason == "" {
		t.Errorf("alerts_raised = %+v", resp.AlertsRaised)
	}
	if resp.Acknowledged.Count != nil || resp.Unacknowledged != nil || *resp.Delivered.Total.Count != 3 {
		t.Errorf("decoded = %+v", resp)
	}
	if string(resp.Coverage) != `{"lifecycle":{"from":"2026-10-01"}}` {
		t.Errorf("coverage = %s", resp.Coverage)
	}
}

func TestGetHealthSummaryUpstreamError(t *testing.T) {
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := GetHealthSummary(context.Background(), c, GetHealthSummaryInput{From: 1, To: 2})
	var apiErr *utils.UpstreamHTTPError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("err = %v", err)
	}
}
