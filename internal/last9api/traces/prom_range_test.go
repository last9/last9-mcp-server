package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestQueryPromRangeRequestAndResponse(t *testing.T) {
	var method, path, authorization string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		authorization = r.Header.Get("X-LAST9-API-TOKEN")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"metric":{"service_name":"api"},"values":[[100,"2"]]}]`))
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:         srv.URL,
		PrometheusReadURL:  "https://prom.example/read",
		PrometheusUsername: "user",
		PrometheusPassword: "pass",
		TokenManager:       &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)},
	}

	got, err := QueryPromRange(context.Background(), last9api.NewClient(srv.Client(), cfg), QueryPromRangeInput{
		Query: "up", Start: 100, End: 220, Step: 30,
		ReadURL: cfg.PrometheusReadURL, Username: cfg.PrometheusUsername, Password: cfg.PrometheusPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/prom_query" {
		t.Errorf("request = %s %s", method, path)
	}
	if authorization != "Bearer token" {
		t.Errorf("authorization = %q", authorization)
	}
	wantBody := map[string]any{
		"query": "up", "timestamp": float64(220), "window": float64(120), "step": float64(30),
		"read_url": "https://prom.example/read", "username": "user", "password": "pass",
	}
	for key, want := range wantBody {
		if body[key] != want {
			t.Errorf("body[%q] = %#v, want %#v", key, body[key], want)
		}
	}
	if len(got) != 1 || got[0].Metric["service_name"] != "api" || len(got[0].Values) != 1 {
		t.Errorf("response = %#v", got)
	}
}

func TestQueryPromRangeSanitizesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"secret upstream detail"}`))
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)},
	}

	_, err := QueryPromRange(context.Background(), last9api.NewClient(srv.Client(), cfg), QueryPromRangeInput{Query: "up", Start: 100, End: 220})
	if err == nil {
		t.Fatal("want non-2xx error")
	}
	if strings.Contains(err.Error(), "secret upstream detail") || !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %q", err)
	}
}
