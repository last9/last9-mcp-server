package services

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

func TestGetServiceProfileRequestAndRawResponse(t *testing.T) {
	for _, raw := range []string{`{"service":"api","future_field":true}`, "null", "", "not JSON"} {
		t.Run(raw, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/service_profile" || r.URL.RawQuery != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("X-Last9-Api-Token") != "Bearer token" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("missing request headers")
				}
				var in GetServiceProfileInput
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				want := GetServiceProfileInput{Service: "api", ReadURL: "https://metrics.example", Username: "user", Password: "secret", Region: "test-region"}
				if in != want {
					t.Errorf("unexpected request fields: %+v", in)
				}
				_, _ = w.Write([]byte(raw))
			}))
			defer server.Close()
			cfg := models.Config{APIBaseURL: server.URL, Region: "test-region", TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
			got, err := GetServiceProfile(context.Background(), last9api.NewClient(server.Client(), cfg), GetServiceProfileInput{Service: "api", ReadURL: "https://metrics.example", Username: "user", Password: "secret", Region: "ignored"})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != raw {
				t.Fatalf("raw response changed: %q", got)
			}
		})
	}
}

func TestGetServiceProfileSanitizesErrorsWithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				http.Error(w, `{"password":"private-secret","read_url":"https://private.example"}`, status)
			}))
			defer server.Close()
			cfg := models.Config{APIBaseURL: server.URL, TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
			_, err := GetServiceProfile(context.Background(), last9api.NewClient(server.Client(), cfg), GetServiceProfileInput{Service: "api"})
			var upstream *utils.UpstreamHTTPError
			if !errors.As(err, &upstream) {
				t.Fatalf("want typed upstream error, got %v", err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("upstream error exposed body: %v", err)
			}
			if calls != 1 {
				t.Fatalf("requests=%d, want one", calls)
			}
		})
	}
}

func TestGetServiceProfileRequiresHTTP200(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusNoContent, http.StatusPartialContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			cfg := models.Config{APIBaseURL: server.URL, TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
			_, err := GetServiceProfile(context.Background(), last9api.NewClient(server.Client(), cfg), GetServiceProfileInput{Service: "api"})
			var upstream *utils.UpstreamHTTPError
			if !errors.As(err, &upstream) {
				t.Fatalf("want HTTP %d rejected, got %v", status, err)
			}
		})
	}
}
