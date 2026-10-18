package prom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestGetLabelValuesRequestAndResponse(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`["checkout","payments"]`))
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		Region:       "ap-south-1",
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}

	values, err := GetLabelValues(context.Background(), last9api.NewClient(srv.Client(), cfg), GetLabelValuesInput{
		Datasource: Datasource{ReadURL: "https://prom.example", Username: "u", Password: "p"},
		Label:      "service_name",
		Matches:    []string{"up"},
		Timestamp:  1700000000,
		Window:     3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(values, []string{"checkout", "payments"}) {
		t.Errorf("values = %v", values)
	}
	if method != http.MethodPost || path != "/prom_label_values" {
		t.Errorf("request = %s %s", method, path)
	}
	want := map[string]any{
		"read_url": "https://prom.example", "username": "u", "password": "p",
		"label": "service_name", "matches": []any{"up"},
		"timestamp": float64(1700000000), "window": float64(3600),
	}
	for key, v := range want {
		if got, _ := json.Marshal(body[key]); string(got) != mustJSON(t, v) {
			t.Errorf("body[%s] = %s, want %s", key, got, mustJSON(t, v))
		}
	}
	if _, ok := body["limit"]; ok {
		t.Errorf("body sends limit %v; zero must be omitted (no limit)", body["limit"])
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
