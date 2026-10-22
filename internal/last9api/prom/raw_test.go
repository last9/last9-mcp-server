package prom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

var testDatasource = Datasource{ReadURL: "https://prom.example", Username: "u", Password: "p"}

type rawCall func(context.Context, *last9api.Client) (json.RawMessage, error)

func rawEndpoints() []struct {
	path     string
	call     rawCall
	wantBody map[string]any
} {
	creds := map[string]any{"read_url": "https://prom.example", "username": "u", "password": "p"}
	with := func(fields map[string]any) map[string]any {
		for k, v := range creds {
			fields[k] = v
		}
		return fields
	}
	return []struct {
		path     string
		call     rawCall
		wantBody map[string]any
	}{
		{"/prom_query", func(ctx context.Context, c *last9api.Client) (json.RawMessage, error) {
			return Query(ctx, c, QueryInput{Datasource: testDatasource, Query: "up", Timestamp: 1700000000, Window: 300})
		}, with(map[string]any{"query": "up", "timestamp": float64(1700000000), "window": float64(300)})},
		{"/prom_query_instant", func(ctx context.Context, c *last9api.Client) (json.RawMessage, error) {
			return QueryInstantRaw(ctx, c, QueryInstantInput{Datasource: testDatasource, Query: "up", Timestamp: 1700000000})
		}, with(map[string]any{"query": "up", "timestamp": float64(1700000000)})},
		{"/prom_label_values", func(ctx context.Context, c *last9api.Client) (json.RawMessage, error) {
			return GetLabelValuesRaw(ctx, c, GetLabelValuesInput{Datasource: testDatasource, Label: "job", Matches: []string{"up"}, Timestamp: 1700000000, Window: 300})
		}, with(map[string]any{"label": "job", "matches": []any{"up"}, "timestamp": float64(1700000000), "window": float64(300)})},
		{"/apm/labels", func(ctx context.Context, c *last9api.Client) (json.RawMessage, error) {
			return GetAPMLabels(ctx, c, GetAPMLabelsInput{Datasource: testDatasource, Metric: "up", Timestamp: 1700000000, Window: 300})
		}, with(map[string]any{"metric": "up", "timestamp": float64(1700000000), "window": float64(300)})},
	}
}

func testClient(t *testing.T, handler http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := models.Config{APIBaseURL: srv.URL, TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}}
	return last9api.NewClient(srv.Client(), cfg)
}

func TestRawEndpointsSendBodyAndPassResponseThrough(t *testing.T) {
	const upstream = `{"status":"success","data":{"z":1,"a":[2]}}`
	for _, ep := range rawEndpoints() {
		t.Run(ep.path, func(t *testing.T) {
			var method, path string
			var body map[string]any
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&body)
				_, _ = w.Write([]byte(upstream))
			})

			raw, err := ep.call(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != upstream {
				t.Errorf("raw = %s, want the upstream body unchanged", raw)
			}
			if method != http.MethodPost || path != ep.path {
				t.Errorf("request = %s %s", method, path)
			}
			if len(body) != len(ep.wantBody) {
				t.Errorf("body = %v, want %v", body, ep.wantBody)
			}
			for key, v := range ep.wantBody {
				if got, _ := json.Marshal(body[key]); string(got) != mustJSON(t, v) {
					t.Errorf("body[%s] = %s, want %s", key, got, mustJSON(t, v))
				}
			}
		})
	}
}

func TestRawEndpointsReturnUpstreamHTTPError(t *testing.T) {
	for _, ep := range rawEndpoints() {
		t.Run(ep.path, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"parse error"}`))
			})

			_, err := ep.call(context.Background(), c)
			var upstream *utils.UpstreamHTTPError
			if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusBadRequest {
				t.Fatalf("err = %v, want *UpstreamHTTPError with HTTP 400", err)
			}
		})
	}
}
