package traces

import (
	"context"
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

func waterfallTestClient(t *testing.T, handler http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		Region:       "ap-south-1",
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	})
}

func TestGetWaterfallRequestAndResponse(t *testing.T) {
	const body = `{"contract_version":"investigation-evidence/v1",  "data":{}}`
	var got *http.Request
	c := waterfallTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, body)
	})

	resp, err := GetWaterfall(context.Background(), c, GetWaterfallInput{
		TraceID: "abcdef0123456789abcdef0123456789", Start: 100, End: 200, MaxSpans: 500,
		SelectedSpanID: "0123456789abcdef", Environment: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != body {
		t.Errorf("body = %s, want it unchanged", resp)
	}
	q := got.URL.Query()
	if got.Method != http.MethodGet || got.URL.Path != "/cat/api/traces/v2/waterfall/abcdef0123456789abcdef0123456789" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
	for key, want := range map[string]string{
		"start": "100", "end": "200", "max_spans": "500",
		"selected_span_id": "0123456789abcdef", "environment": "prod", "region": "ap-south-1",
	} {
		if q.Get(key) != want {
			t.Errorf("query %s = %q, want %q (url %s)", key, q.Get(key), want, got.URL)
		}
	}
}

func TestGetWaterfallOmitsBlankOptionalParams(t *testing.T) {
	var got *http.Request
	c := waterfallTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, `{}`)
	})
	if _, err := GetWaterfall(context.Background(), c, GetWaterfallInput{TraceID: "t", Start: 1, End: 2, MaxSpans: 1}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"selected_span_id", "environment"} {
		if got.URL.Query().Has(key) {
			t.Errorf("%s sent when blank: %s", key, got.URL)
		}
	}
}

func TestGetWaterfallCapsTheBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"at cap", MaxWaterfallBytes, false},
		{"cap+1", MaxWaterfallBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A JSON string of exactly tc.size bytes.
			body := `"` + strings.Repeat("a", tc.size-2) + `"`
			c := waterfallTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			resp, err := GetWaterfall(context.Background(), c, GetWaterfallInput{TraceID: "t", Start: 1, End: 2, MaxSpans: 1})
			var tooLarge *last9api.ResponseTooLargeError
			if tc.wantErr {
				if !errors.As(err, &tooLarge) || tooLarge.MaxBytes != MaxWaterfallBytes {
					t.Fatalf("want *ResponseTooLargeError at %d bytes, got %v", MaxWaterfallBytes, err)
				}
				return
			}
			if err != nil || len(resp) != tc.size {
				t.Fatalf("len = %d, err = %v; want the whole %d-byte body", len(resp), err, tc.size)
			}
		})
	}
}

func TestGetWaterfallMapsNon2xx(t *testing.T) {
	c := waterfallTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := GetWaterfall(context.Background(), c, GetWaterfallInput{TraceID: "t", Start: 1, End: 2, MaxSpans: 1})
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusNotFound || !strings.Contains(err.Error(), "trace waterfall") {
		t.Fatalf("want a 404 UpstreamHTTPError naming the op, got %v", err)
	}
}
