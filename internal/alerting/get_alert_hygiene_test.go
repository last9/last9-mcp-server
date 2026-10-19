package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type hygieneStub struct {
	rules    []alerts.HygieneRule
	status   int
	from     int64 // response from; zero means testWindowFr
	requests atomic.Int32
	window   atomic.Value
}

func (s *hygieneStub) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	s.requests.Add(1)
	s.window.Store(r.URL.Query().Get("window"))
	if r.URL.Path != "/alert-hygiene" {
		http.NotFound(w, r)
		return
	}
	if s.status != 0 {
		w.WriteHeader(s.status)
		return
	}
	from := s.from
	if from == 0 {
		from = testWindowFr
	}
	_ = json.NewEncoder(w).Encode(alerts.GetAlertHygieneResponse{
		Window: r.URL.Query().Get("window"), From: from, To: testWindowTo,
		Rules: append([]alerts.HygieneRule{}, s.rules...),
	})
}

func callGetAlertHygiene(t *testing.T, stub *hygieneStub, args GetAlertHygieneArgs) (*mcp.CallToolResult, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL: srv.URL,
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
	result, _, err := NewGetAlertHygieneHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	return result, err
}

func decodeHygieneResult(t *testing.T, result *mcp.CallToolResult) alertHygieneResult {
	t.Helper()
	var out alertHygieneResult
	body := utils.GetTextContent(t, result)
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, body)
	}
	return out
}

func unroutedRules(n int) []alerts.HygieneRule {
	rules := make([]alerts.HygieneRule, 0, n)
	for i := range n {
		r := healthyRule(fmt.Sprintf("r%02d", i))
		r.Routed = boolp(false)
		r.FireCount = int64(i)
		rules = append(rules, r)
	}
	return rules
}

func TestGetAlertHygiene_RejectsInvalidArgsBeforeRequest(t *testing.T) {
	for _, args := range []GetAlertHygieneArgs{
		{Window: "14d"},
		{Window: "7"},
		{Limit: -1},
		{Limit: maxHygieneLimit + 1},
	} {
		t.Run(fmt.Sprintf("%+v", args), func(t *testing.T) {
			stub := &hygieneStub{}
			if _, err := callGetAlertHygiene(t, stub, args); err == nil {
				t.Fatal("expected a validation error")
			}
			if n := stub.requests.Load(); n != 0 {
				t.Fatalf("made %d requests for invalid args", n)
			}
		})
	}
}

func TestGetAlertHygiene_RequestsTheWindow(t *testing.T) {
	for _, tc := range []struct{ arg, want string }{
		{"", "7d"},
		{"7d", "7d"},
		{"30d", "30d"},
		{" 30D ", "30d"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			stub := &hygieneStub{}
			result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{Window: tc.arg})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if got := stub.window.Load(); got != tc.want {
				t.Errorf("window query = %v, want %s", got, tc.want)
			}
			if got := decodeHygieneResult(t, result).Window; got != tc.want {
				t.Errorf("result window = %q, want %s", got, tc.want)
			}
		})
	}
}

func TestGetAlertHygiene_LimitsRankedRules(t *testing.T) {
	cases := []struct {
		name      string
		flagged   int
		limit     int
		returned  int
		truncated bool
	}{
		{"default limit truncates", defaultHygieneLimit + 1, 0, defaultHygieneLimit, true},
		{"default limit exact fill", defaultHygieneLimit, 0, defaultHygieneLimit, false},
		{"explicit limit", 5, 2, 2, true},
		{"limit above flagged", 3, 4, 3, false},
		{"max limit", maxHygieneLimit + 1, maxHygieneLimit, maxHygieneLimit, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := append(unroutedRules(tc.flagged), healthyRule("healthy"))
			stub := &hygieneStub{rules: rules}
			result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{Limit: tc.limit})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeHygieneResult(t, result)
			if len(out.Rules) != tc.returned || out.Truncated != tc.truncated {
				t.Fatalf("returned %d truncated=%v, want %d truncated=%v", len(out.Rules), out.Truncated, tc.returned, tc.truncated)
			}
			if out.RulesEvaluated != tc.flagged+1 || out.FlaggedRules != tc.flagged {
				t.Errorf("rules_evaluated=%d flagged_rules=%d", out.RulesEvaluated, out.FlaggedRules)
			}
		})
	}
}

func TestGetAlertHygiene_ReturnsRankedFindingsWithStats(t *testing.T) {
	stub := &hygieneStub{rules: unroutedRules(3)}
	result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeHygieneResult(t, result)
	got := []string{}
	for _, v := range out.Rules {
		got = append(got, v.Stats.RuleID)
		if len(v.Findings) != 1 || v.Findings[0].Category != hygieneCategoryUnrouted {
			t.Errorf("%s findings = %+v", v.Stats.RuleID, v.Findings)
		}
	}
	if want := []string{"r02", "r01", "r00"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if out.From != time.Unix(testWindowFr, 0).UTC().Format(time.RFC3339) {
		t.Errorf("from = %q", out.From)
	}
	if result.Meta["reference_url"] == nil {
		t.Errorf("missing reference_url meta: %v", result.Meta)
	}
}

func TestGetAlertHygiene_NoFlaggedRules(t *testing.T) {
	stub := &hygieneStub{rules: []alerts.HygieneRule{healthyRule("ok")}}
	result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeHygieneResult(t, result)
	if out.Rules == nil || len(out.Rules) != 0 || out.FlaggedRules != 0 || out.RulesEvaluated != 1 {
		t.Fatalf("result = %+v", out)
	}
}

func TestGetAlertHygiene_UpstreamError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			stub := &hygieneStub{status: status}
			result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{})
			if err == nil {
				t.Fatalf("expected upstream error, got %+v", result)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(status)) {
				t.Errorf("error %q does not name HTTP %d", err, status)
			}
		})
	}
}

// A route the deployment does not serve yet must never read as "no rules to fix".
func TestGetAlertHygiene_NotFoundSaysNothingWasChecked(t *testing.T) {
	stub := &hygieneStub{status: http.StatusNotFound}
	_, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{})
	if err == nil || !strings.Contains(err.Error(), "no alert rules were checked") {
		t.Fatalf("err = %v, want it to say no alert rules were checked", err)
	}
}

// Each null field skips or weakens a check, so the result counts them instead of
// letting "no noisy rules" hide that the inputs were missing.
func TestGetAlertHygiene_CountsNullFieldsOfEnabledRules(t *testing.T) {
	unacked := healthyRule("unacked")
	unacked.Ack = nil
	unacked.MutedSeconds = nil
	unknownRouting := healthyRule("unknown-routing")
	unknownRouting.Routed = nil
	noHistory := healthyRule("no-history")
	noHistory.HistorySince = nil
	disabled := alerts.HygieneRule{RuleID: "disabled", IsDisabled: true}

	stub := &hygieneStub{rules: []alerts.HygieneRule{unacked, unknownRouting, noHistory, disabled, healthyRule("ok")}}
	result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := hygieneNullFields{Ack: 1, MutedSeconds: 1, Routed: 1, HistorySince: 1}
	if got := decodeHygieneResult(t, result).NullFields; got != want {
		t.Errorf("null_fields = %+v, want %+v", got, want)
	}
	const wantJSON = `"null_fields":{"ack":1,"muted_seconds":1,"routed":1,"history_since":1}`
	if body := utils.GetTextContent(t, result); !strings.Contains(body, wantJSON) {
		t.Errorf("payload does not carry %s:\n%s", wantJSON, body)
	}
}

// Alert history is retained for about 21 days, so the API starts a 30d window
// late; the result must say how much of the window was actually covered.
func TestGetAlertHygiene_ReportsTheCoveredWindow(t *testing.T) {
	cases := []struct {
		name        string
		window      string
		from        int64
		coveredDays float64
		partial     bool
	}{
		{"7d fully covered", "7d", testWindowTo - 7*testDay, 7, false},
		{"30d fully covered", "30d", testWindowTo - 30*testDay, 30, false},
		{"30d cut to retention", "30d", testWindowTo - 21*testDay, 21, true},
		{"one second short", "7d", testWindowTo - 7*testDay + 1, 7, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &hygieneStub{from: tc.from}
			result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{Window: tc.window})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeHygieneResult(t, result)
			if out.CoveredDays != tc.coveredDays || out.PartialWindow != tc.partial {
				t.Errorf("covered_days=%v partial_window=%v, want %v %v", out.CoveredDays, out.PartialWindow, tc.coveredDays, tc.partial)
			}
			if want := time.Unix(tc.from, 0).UTC().Format(time.RFC3339); out.From != want {
				t.Errorf("from = %q, want the covered start %q", out.From, want)
			}
		})
	}
}
