package prom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestQueryInstantRequestAndResponse(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`[{"metric":{"service_name":"api"},"value":[1700000000,"4"]}]`))
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{APIBaseURL: srv.URL, TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}}

	series, err := QueryInstant(context.Background(), last9api.NewClient(srv.Client(), cfg), QueryInstantInput{
		Datasource: Datasource{ReadURL: "https://prom.example", Username: "u", Password: "p"},
		Query:      "up",
		Timestamp:  1700000000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Metric["service_name"] != "api" || series[0].Value[1] != "4" {
		t.Errorf("series = %+v", series)
	}
	if method != http.MethodPost || path != "/prom_query_instant" {
		t.Errorf("request = %s %s", method, path)
	}
	if body["query"] != "up" || body["timestamp"] != float64(1700000000) || body["read_url"] != "https://prom.example" || body["username"] != "u" || body["password"] != "p" {
		t.Errorf("body = %v", body)
	}
}
