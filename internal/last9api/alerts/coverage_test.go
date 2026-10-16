package alerts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func newCoverageTestClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
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

func TestGetAlertCoverage_Query(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   GetAlertCoverageInput
		want url.Values
	}{
		{"none", GetAlertCoverageInput{}, url.Values{}},
		{"muted days only", GetAlertCoverageInput{MutedDays: 7}, url.Values{"muted_days": {"7"}}},
		{"all", GetAlertCoverageInput{Service: "checkout", Environment: "prod", EntityID: "ent-1", MutedDays: 30},
			url.Values{"service": {"checkout"}, "environment": {"prod"}, "entity_id": {"ent-1"}, "muted_days": {"30"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var method, path string
			var query url.Values
			c := newCoverageTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				method, path, query = r.Method, r.URL.Path, r.URL.Query()
				_, _ = w.Write([]byte(`{"generated_at":1,"muted_days":7,"gaps":[],"counts":{}}`))
			})

			if _, err := GetAlertCoverage(context.Background(), c, tc.in); err != nil {
				t.Fatalf("GetAlertCoverage: %v", err)
			}
			if method != http.MethodGet || path != "/alerts/coverage" {
				t.Errorf("request = %s %s, want GET /alerts/coverage", method, path)
			}
			if query.Encode() != tc.want.Encode() {
				t.Errorf("query = %q, want %q", query.Encode(), tc.want.Encode())
			}
		})
	}
}

func TestGetAlertCoverage_DecodesGaps(t *testing.T) {
	c := newCoverageTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"generated_at":1700000000,"muted_days":14,
			"gaps":[{"kind":"muted_too_long","entity_id":"ent-1","entity_name":"Checkout","service":"checkout",
				"rule_id":"rule-9","rule_name":"p99 latency","severity":"breach","muted_since":1690000000,"mute_until":0,
				"detail":"muted 20 days","suggested_tool":"patch_alert"}],
			"counts":{"uncovered_signal":0,"all_rules_disabled":0,"no_notification_channel":0,"muted_too_long":1}}`))
	})

	resp, err := GetAlertCoverage(context.Background(), c, GetAlertCoverageInput{MutedDays: 14})
	if err != nil {
		t.Fatalf("GetAlertCoverage: %v", err)
	}
	if resp.GeneratedAt != 1700000000 || resp.MutedDays != 14 || len(resp.Gaps) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	want := CoverageGap{
		Kind: CoverageGapMutedTooLong, EntityID: "ent-1", EntityName: "Checkout", Service: "checkout",
		RuleID: "rule-9", RuleName: "p99 latency", Severity: "breach", MutedSince: 1690000000,
		Detail: "muted 20 days", SuggestedTool: "patch_alert",
	}
	if resp.Gaps[0] != want {
		t.Errorf("gap = %+v, want %+v", resp.Gaps[0], want)
	}
	if resp.Counts[CoverageGapMutedTooLong] != 1 || len(resp.Counts) != 4 {
		t.Errorf("counts = %v", resp.Counts)
	}
}

func TestGetAlertCoverage_UpstreamErrorSurfaces(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newCoverageTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"internal detail"}`))
			})

			_, err := GetAlertCoverage(context.Background(), c, GetAlertCoverageInput{})
			if err == nil {
				t.Fatalf("expected an error for HTTP %d", status)
			}
			if !strings.Contains(err.Error(), "alert coverage") || strings.Contains(err.Error(), "internal detail") {
				t.Errorf("error = %v, want op named and body omitted", err)
			}
		})
	}
}

func TestGetAlertCoverage_BodyCap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"at cap", maxCoverageResponseBytes, false},
		{"past cap", maxCoverageResponseBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCoverageTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write([]byte(paddedCoverageBody(tc.size)))
			})

			_, err := GetAlertCoverage(context.Background(), c, GetAlertCoverageInput{})
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// Padding sits inside a gap's detail so the body stays a well-formed coverage response.
func paddedCoverageBody(size int) string {
	const prefix = `{"generated_at":1,"muted_days":7,"gaps":[{"kind":"uncovered_signal","detail":"`
	const suffix = `"}],"counts":{"uncovered_signal":1}}`
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}
