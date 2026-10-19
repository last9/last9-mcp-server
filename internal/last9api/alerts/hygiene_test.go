package alerts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func testClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL: srv.URL,
		OrgSlug:    "test-org",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
	return last9api.NewClient(srv.Client(), cfg)
}

func TestGetAlertHygiene_RequestPerWindow(t *testing.T) {
	for _, window := range []string{HygieneWindow7d, HygieneWindow30d} {
		t.Run(window, func(t *testing.T) {
			var method, path, gotWindow string
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				method, path, gotWindow = r.Method, r.URL.Path, r.URL.Query().Get("window")
				_, _ = w.Write([]byte(`{"window":"` + window + `","from":1,"to":2,"rules":[]}`))
			})

			resp, err := GetAlertHygiene(context.Background(), c, GetAlertHygieneInput{Window: window})
			if err != nil {
				t.Fatalf("GetAlertHygiene: %v", err)
			}
			if method != http.MethodGet || path != "/alert-hygiene" {
				t.Errorf("request = %s %s, want GET /alert-hygiene", method, path)
			}
			if gotWindow != window {
				t.Errorf("window query = %q, want %q", gotWindow, window)
			}
			if resp.Window != window {
				t.Errorf("response window = %q", resp.Window)
			}
		})
	}
}

func TestGetAlertHygiene_DecodesNullHistoryAsNil(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"window":"7d","from":1000,"to":2000,"rules":[
			{"rule_id":"r-null","fire_count":0,"last_fired_at":null,"ack":null,"muted_seconds":null,"history_since":null,"routed":null,"channels":[]},
			{"rule_id":"r-zero","expression":"error_rate","condition":"expr > 5","fire_count":3,"last_fired_at":1500,"ack":{"acked_episodes":0,"ack_rate":0,"median_seconds_to_ack":null},"muted_seconds":0,"history_since":900,"routed":true,
			 "channels":[{"type":"slack","name":"oncall"}]},
			{"rule_id":"r-absent","fire_count":0}
		]}`))
	})

	resp, err := GetAlertHygiene(context.Background(), c, GetAlertHygieneInput{Window: HygieneWindow7d})
	if err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if resp.From != 1000 || resp.To != 2000 || len(resp.Rules) != 3 {
		t.Fatalf("resp = %+v", resp)
	}
	null := resp.Rules[0]
	if null.LastFiredAt != nil || null.Ack != nil || null.MutedSeconds != nil || null.HistorySince != nil || null.Routed != nil {
		t.Errorf("null history decoded as values: %+v", null)
	}
	if absent := resp.Rules[2]; absent.Routed != nil {
		t.Errorf("absent routed decoded as %v, want nil", *absent.Routed)
	}
	zero := resp.Rules[1]
	if zero.Ack == nil || zero.Ack.AckRate != 0 || zero.Ack.MedianSecondsToAck != nil {
		t.Errorf("ack = %+v, want recorded zero rate", zero.Ack)
	}
	if zero.MutedSeconds == nil || *zero.MutedSeconds != 0 {
		t.Errorf("muted_seconds = %v, want recorded zero", zero.MutedSeconds)
	}
	if zero.LastFiredAt == nil || *zero.LastFiredAt != 1500 || zero.HistorySince == nil || *zero.HistorySince != 900 {
		t.Errorf("timestamps = %v %v", zero.LastFiredAt, zero.HistorySince)
	}
	if zero.Expression != "error_rate" || zero.Condition != "expr > 5" {
		t.Errorf("expression, condition = %q, %q", zero.Expression, zero.Condition)
	}
	if zero.Routed == nil || !*zero.Routed {
		t.Errorf("routed = %v, want recorded true", zero.Routed)
	}
	if len(zero.Channels) != 1 || zero.Channels[0].Type != "slack" || zero.Channels[0].Name != "oncall" {
		t.Errorf("channels = %+v", zero.Channels)
	}
}

func TestGetAlertHygiene_UpstreamErrorSurfaces(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := GetAlertHygiene(context.Background(), c, GetAlertHygieneInput{Window: HygieneWindow7d}); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}
