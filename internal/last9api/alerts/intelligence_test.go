package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func newTestClient(srv *httptest.Server) *last9api.Client {
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
	})
}

func TestCreateAlertFromChartRequestAndResponse(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/alert_intelligence" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if got := r.Header.Get("X-LAST9-API-TOKEN"); got != "Bearer test-token" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"rule-1","signals":[{"key":"p95_latency","eval_query":"expr","unit":"ms","query_kind":"metric"}],"group_id":"grp-1","kpi_id":"kpi-1"}`))
	}))
	defer srv.Close()

	raw, err := CreateAlertFromChart(context.Background(), newTestClient(srv), IntelligenceRequest{ChartKey: "chart-1", SignalKey: "p95_latency"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(gotBody, []byte(`"operation":"create_from_chart"`)) {
		t.Errorf("request body missing operation: %s", gotBody)
	}
	var resp IntelligenceResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != "rule-1" || resp.GroupID != "grp-1" || resp.KPIID != "kpi-1" || len(resp.Signals) != 1 || resp.Signals[0].Key != "p95_latency" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestDescribeAlertChartSetsOperation(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := DescribeAlertChart(context.Background(), newTestClient(srv), IntelligenceRequest{Operation: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(gotBody, []byte(`"operation":"describe_chart"`)) {
		t.Errorf("request body missing operation: %s", gotBody)
	}
}
