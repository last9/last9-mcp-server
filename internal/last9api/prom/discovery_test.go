package prom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/apitest"
	"last9-mcp/internal/models"
)

func TestDiscoverLabelValues(t *testing.T)         { testDiscoverLabelValues(t, false) }
func TestDiscoverLabelValuesInMemory(t *testing.T) { testDiscoverLabelValues(t, true) }
func testDiscoverLabelValues(t *testing.T, inMemory bool) {
	input := GetLabelValuesInput{Datasource: Datasource{ReadURL: "https://selected.example", Username: "selected", Password: "test"}, Label: "__name__", Matches: []string{`{env="prod"}`}, Timestamp: 1700003600, Window: 3600}
	for _, tt := range []struct {
		name, body string
		status     int
		want       []string
		bad        bool
	}{
		{"names", `["checkout_requests","checkout_errors"]`, 200, []string{"checkout_requests", "checkout_errors"}, false},
		{"empty", `[]`, 200, []string{}, false},
		{"no body", ``, 200, nil, true},
		{"no content", ``, 204, nil, true},
		{"null", `null`, 200, nil, true},
		{"object", `{}`, 200, nil, true},
		{"partial envelope", `{"data":["checkout_requests"],"partial":true}`, 200, nil, true},
		{"trailing data", `["checkout_requests"] []`, 200, nil, true},
		{"null entry", `["checkout_requests",null]`, 200, nil, true},
		{"blank entry", `[""]`, 200, nil, true},
		{"upstream failure", `failure`, 503, nil, true},
		{"body cap", `["` + strings.Repeat("x", 1<<20) + `"]`, 200, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, client := apitest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/prom_label_values" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				var got GetLabelValuesInput
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(got, input) {
					t.Errorf("scope = %+v; want %+v", got, input)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}), inMemory)
			cfg := models.Config{APIBaseURL: baseURL, TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
			got, err := DiscoverLabelValues(context.Background(), last9api.NewClient(client, cfg), input)
			if (err != nil) != tt.bad {
				t.Fatalf("values count=%d error=%v", len(got), err)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("values=%v; want %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverLabelValuesDeadline(t *testing.T)         { testDiscoverLabelValuesDeadline(t, false) }
func TestDiscoverLabelValuesDeadlineInMemory(t *testing.T) { testDiscoverLabelValuesDeadline(t, true) }
func testDiscoverLabelValuesDeadline(t *testing.T, inMemory bool) {
	baseURL, client := apitest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}), inMemory)
	cfg := models.Config{APIBaseURL: baseURL, TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := DiscoverLabelValues(ctx, last9api.NewClient(client, cfg), GetLabelValuesInput{Label: "__name__"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}
