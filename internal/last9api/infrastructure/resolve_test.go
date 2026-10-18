package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

func testClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL: srv.URL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})
}

func testInput() ResolveInfrastructureInput {
	return ResolveInfrastructureInput{
		ClusterID:  "cluster-uuid",
		EntityType: "host",
		Timestamp:  1700000000,
		Selectors:  map[string]string{"host_name": "web-1"},
	}
}

func TestResolveInfrastructure_RequestShape(t *testing.T) {
	var method, path, region, query string
	var body ResolveInfrastructureInput
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, region, query = r.Method, r.URL.Path, r.Header.Get("region"), r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"status":"matched"}`))
	})

	out, err := ResolveInfrastructure(context.Background(), c, testInput())
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/infrastructure/resolve" {
		t.Errorf("request = %s %s", method, path)
	}
	if region != "ap-south-1" || query != "" {
		t.Errorf("region header = %q, query = %q; want region in the header only", region, query)
	}
	if body.ClusterID != "cluster-uuid" || body.EntityType != "host" || body.Timestamp != 1700000000 || body.Selectors["host_name"] != "web-1" {
		t.Errorf("body = %+v", body)
	}
	if string(out) != `{"status":"matched"}` {
		t.Errorf("response = %s, want it verbatim", out)
	}
}

func TestResolveInfrastructure_ResponseCap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"at cap", maxResolveResponseBytes, false},
		{"over cap", maxResolveResponseBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"pad":"` + strings.Repeat("x", tc.size-len(`{"pad":""}`)) + `"}`
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(payload))
			})
			_, err := ResolveInfrastructure(context.Background(), c, testInput())
			if tc.wantErr != (err != nil) {
				t.Fatalf("size %d: err = %v, wantErr %v", tc.size, err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "response exceeds") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestResolveInfrastructure_UpstreamErrorIsTyped(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad selectors"}`, http.StatusBadRequest)
	})
	_, err := ResolveInfrastructure(context.Background(), c, testInput())
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusBadRequest {
		t.Fatalf("err = %v, want UpstreamHTTPError 400", err)
	}
	if !strings.Contains(err.Error(), "bad selectors") {
		t.Errorf("400 body should be relayed sanitized: %v", err)
	}
}
