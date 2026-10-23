package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func newClient(srvURL string, httpClient *http.Client) *last9api.Client {
	return last9api.NewClient(httpClient, models.Config{
		APIBaseURL:   srvURL,
		Region:       "ap-south-1",
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	})
}

func TestGetDashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/dashboards/a%2Fb" || r.URL.RawQuery != "region=ap-south-1" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"dashboard":{"id":"a/b","name":"N"},"metadata":{"_type":"metrics"}}`)
	}))
	defer srv.Close()

	got, err := GetDashboard(context.Background(), newClient(srv.URL, srv.Client()), "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dashboard["name"] != "N" || string(got.Metadata) != `{"_type":"metrics"}` {
		t.Fatalf("got %+v", got)
	}
}

func TestGetDashboardErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
	}{
		{"not found", http.StatusNotFound, `{"error":"x"}`, func(err error) bool {
			var u *utils.UpstreamHTTPError
			return errors.As(err, &u) && u.StatusCode == http.StatusNotFound
		}},
		{"oversized", http.StatusOK, `{"dashboard":{"name":"` + strings.Repeat("x", maxDashboardBytes) + `"}}`, func(err error) bool {
			var tooLarge *last9api.ResponseTooLargeError
			return errors.As(err, &tooLarge)
		}},
		{"malformed", http.StatusOK, `{"dashboard":`, func(err error) bool { return err != nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			if _, err := GetDashboard(context.Background(), newClient(srv.URL, srv.Client()), "d1"); !tt.check(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestUpdateDashboard(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/dashboards/d1" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery)
		}
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = io.WriteString(w, `{"dashboard":{"id":"d1"}}`)
	}))
	defer srv.Close()

	body := json.RawMessage(`{"dashboard":{"id":"d1"},"metadata":{"_type":"metrics"}}`)
	if err := UpdateDashboard(context.Background(), newClient(srv.URL, srv.Client()), "d1", body); err != nil {
		t.Fatal(err)
	}
	if gotBody != string(body) {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestUpdateDashboardForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	err := UpdateDashboard(context.Background(), newClient(srv.URL, srv.Client()), "d1", json.RawMessage(`{}`))
	var u *utils.UpstreamHTTPError
	if !errors.As(err, &u) || u.StatusCode != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
}
