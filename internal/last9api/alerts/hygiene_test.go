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

var testHygieneInput = GetAlertHygieneInput{
	From:     time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC),
	To:       time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	Timezone: "UTC",
}

func TestGetAlertHygiene_SendsRFC3339RangeAndTimezone(t *testing.T) {
	var method, path, from, to, timezone string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		q := r.URL.Query()
		method, path, from, to, timezone = r.Method, r.URL.Path, q.Get("from"), q.Get("to"), q.Get("timezone")
		_, _ = w.Write([]byte(`{"rules":[]}`))
	})

	if _, err := GetAlertHygiene(context.Background(), c, testHygieneInput); err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if method != http.MethodGet || path != "/alerts/hygiene" {
		t.Errorf("request = %s %s, want GET /alerts/hygiene", method, path)
	}
	if from != "2026-10-27T00:00:00Z" || to != "2026-11-03T00:00:00Z" || timezone != "UTC" {
		t.Errorf("from, to, timezone = %q, %q, %q", from, to, timezone)
	}
}

func TestGetAlertHygiene_DecodesAvailabilityCoverageAndTruncation(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{
			"period":{"from":"2026-10-27T00:00:00Z","to":"2026-11-03T00:00:00Z","timezone":"UTC"},
			"coverage":{"lifecycle":{"status":"partial","range":{"from":"2026-10-30T00:00:00Z","to":"2026-11-03T00:00:00Z"},"reason":"retention"},
			            "delivery":{"status":"unavailable","reason":"no ledger"}},
			"rules":[
				{"rule_id":"r-zero","alert_group_id":"g1","fire_count":{"status":"available","count":0},
				 "flaps_per_day":{"status":"available","value":0},"ack_rate":{"status":"available","value":0},
				 "last_fired_at":"2026-10-01T00:00:00Z","routed":false},
				{"rule_id":"r-unavailable","fire_count":{"status":"unavailable","reason":"no ledger"},
				 "flaps_per_day":{"status":"unavailable"},"ack_rate":{"status":"unavailable"}}
			],
			"truncated":true}`))
	})

	resp, err := GetAlertHygiene(context.Background(), c, testHygieneInput)
	if err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if !resp.Truncated || !resp.Period.To.Equal(testHygieneInput.To) || len(resp.Rules) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if lc := resp.Coverage.Lifecycle; lc.Status != HygieneStatusPartial || lc.Range == nil || lc.Range.From.Day() != 30 {
		t.Errorf("lifecycle coverage = %+v", lc)
	}
	zero := resp.Rules[0]
	if zero.FireCount.Count == nil || *zero.FireCount.Count != 0 || zero.AckRate.Value == nil || *zero.AckRate.Value != 0 {
		t.Errorf("observed zeros decoded as %+v", zero)
	}
	if zero.LastFiredAt == nil || zero.Routed == nil || *zero.Routed {
		t.Errorf("last_fired_at, routed = %v, %v", zero.LastFiredAt, zero.Routed)
	}
	unavailable := resp.Rules[1]
	if unavailable.FireCount.Count != nil || unavailable.FlapsPerDay.Value != nil || unavailable.AckRate.Value != nil ||
		unavailable.LastFiredAt != nil || unavailable.Routed != nil {
		t.Errorf("unavailable values decoded as observed: %+v", unavailable)
	}
}

// The tool reads an empty rules list as "nothing to fix", so a body without one must fail.
func TestGetAlertHygiene_RejectsResponseWithoutRules(t *testing.T) {
	for _, body := range []string{``, `null`, `{}`, `{"truncated":false}`, `{"rules":null}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write([]byte(body))
			})
			if resp, err := GetAlertHygiene(context.Background(), c, testHygieneInput); err == nil {
				t.Fatalf("expected an error, got %+v", resp)
			}
		})
	}
}

func TestGetAlertHygiene_AcceptsEmptyRules(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"rules":[]}`))
	})
	resp, err := GetAlertHygiene(context.Background(), c, testHygieneInput)
	if err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if resp.Rules == nil || len(resp.Rules) != 0 {
		t.Fatalf("rules = %#v, want empty", resp.Rules)
	}
}

func TestGetAlertHygiene_UpstreamErrorSurfaces(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := GetAlertHygiene(context.Background(), c, testHygieneInput); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}
