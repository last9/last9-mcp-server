package prom

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestQuerySeriesSendsBodyAndDecodesResponse(t *testing.T) {
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/prom_query" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"metric":{"service_name":"api"},"values":[[100,"2"]]}]`))
	})

	got, err := QuerySeries(context.Background(), c, QueryInput{
		Datasource: testDatasource,
		Query:      "up",
		Timestamp:  220.9,
		Window:     120.5,
		Step:       30,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBody := map[string]any{
		"query": "up", "timestamp": 220.9, "window": 120.5, "step": float64(30),
		"read_url": "https://prom.example", "username": "u", "password": "p",
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

func TestQuerySeriesRequiresBodyAndSanitizesNon2xx(t *testing.T) {
	t.Run("empty success", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {})
		_, err := QuerySeries(context.Background(), c, QueryInput{Query: "up"})
		if err == nil {
			t.Fatal("want required-body error")
		}
	})

	t.Run("non-2xx", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"secret upstream detail"}`))
		})
		_, err := QuerySeries(context.Background(), c, QueryInput{Query: "up"})
		if err == nil {
			t.Fatal("want non-2xx error")
		}
		if strings.Contains(err.Error(), "secret upstream detail") || !strings.Contains(err.Error(), "502") {
			t.Errorf("error = %q", err)
		}
	})
}
