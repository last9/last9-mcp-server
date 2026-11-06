package prom

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

func TestGetMetadata(t *testing.T) {
	const success = `{"status":"success","data":{"requests_total":[{"type":"counter","unit":"","help":"Requests"},{"type":"gauge","unit":"","help":"Conflict"}]}}`
	for _, tt := range []struct {
		name, body        string
		status            int
		fail, unsupported bool
	}{
		{"definitions", success, 200, false, false},
		{"missing", `{"status":"success","data":{}}`, 200, false, false},
		{"empty", "", 200, true, false},
		{"null", "null", 200, true, false},
		{"missing data", `{"status":"success"}`, 200, true, false},
		{"wrong status", `{"status":"error","data":{}}`, 200, true, false},
		{"empty definitions", `{"status":"success","data":{"requests_total":[]}}`, 200, false, false},
		{"incomplete", `{"status":"success","data":{"requests_total":[{"type":"counter"}]}}`, 200, true, false},
		{"other metric", strings.ReplaceAll(success, "requests_total", "other"), 200, true, false},
		{"trailing", success + ` {}`, 200, true, false},
		{"at cap", success + strings.Repeat(" ", (256<<10)-len(success)), 200, false, false},
		{"over cap", success + strings.Repeat(" ", (256<<10)-len(success)+1), 200, true, false},
		{"unsupported", `{"status":501,"error":"datasource metadata endpoint is unavailable or unsupported"}`, 501, true, true},
		{"missing route", `{}`, 404, true, false},
		{"unauthorized", `{}`, 401, true, false},
		{"forbidden", `{}`, 403, true, false},
		{"operational", `{}`, 503, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var request GetMetadataInput
			var method, path, token string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path, token = r.Method, r.URL.RequestURI(), r.Header.Get("X-LAST9-API-TOKEN")
				_ = json.NewDecoder(r.Body).Decode(&request)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			cfg := models.Config{APIBaseURL: srv.URL, TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
			in := GetMetadataInput{Datasource: Datasource{ReadURL: "https://prom.example", Username: "u", Password: "p"}, Metric: "requests_total"}
			result, err := GetMetadata(context.Background(), last9api.NewClient(srv.Client(), cfg), in)
			if (err != nil) != tt.fail {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if errors.Is(err, ErrMetadataUnsupported) != tt.unsupported {
				t.Fatalf("unsupported: %v", err)
			}
			if method != "POST" || path != "/prom_metadata" || token != "Bearer test" || request != in {
				t.Fatalf("request: %s %s %s %+v", method, path, token, request)
			}
			if tt.name == "definitions" && len(result.Data[in.Metric]) != 2 {
				t.Fatalf("lost definitions: %+v", result)
			}
			if tt.status >= 400 && !tt.unsupported {
				var upstream *utils.UpstreamHTTPError
				if !errors.As(err, &upstream) || upstream.StatusCode != tt.status {
					t.Fatalf("lost status: %v", err)
				}
			}
		})
	}
}

func TestGetMetadataRejectsNamesBeforeIO(t *testing.T) {
	for _, name := range []string{"", " ", "up{}", "rate(up[5m])", "up\n"} {
		_, err := GetMetadata(context.Background(), last9api.NewClient(nil, models.Config{}), GetMetadataInput{Metric: name})
		if err == nil || !strings.Contains(err.Error(), "metric name") {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestGetMetadataCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := models.Config{APIBaseURL: "https://example.invalid", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
	_, err := GetMetadata(ctx, last9api.NewClient(http.DefaultClient, cfg), GetMetadataInput{Metric: "up"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}
