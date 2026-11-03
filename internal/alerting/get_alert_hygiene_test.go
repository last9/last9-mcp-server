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

// hygieneStub serves the hygiene API and the alert rules list the tool joins it with.
type hygieneStub struct {
	rules       []hygieneRule
	quiet       []AlertRule                   // configured rules the hygiene API has no row for
	lifecycle   *alerts.HygieneCoverageSource // nil means complete
	status      int                           // hygiene API status; zero means 200
	rulesStatus int                           // alert rules list status; zero means 200
	truncated   bool
	requests    atomic.Int32
	from, to    atomic.Value
}

func (s *hygieneStub) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	s.requests.Add(1)
	switch r.URL.Path {
	case "/alerts/hygiene":
		s.from.Store(r.URL.Query().Get("from"))
		s.to.Store(r.URL.Query().Get("to"))
		if s.status != 0 {
			w.WriteHeader(s.status)
			return
		}
		lifecycle := alerts.HygieneCoverageSource{Status: alerts.HygieneStatusComplete}
		if s.lifecycle != nil {
			lifecycle = *s.lifecycle
		}
		resp := alerts.GetAlertHygieneResponse{
			Period: alerts.HygienePeriod{From: testWindowFr, To: testWindowTo, Timezone: "UTC"},
			Coverage: alerts.HygieneCoverage{
				Lifecycle: lifecycle,
				Delivery:  alerts.HygieneCoverageSource{Status: alerts.HygieneStatusComplete},
			},
			Rules:     []alerts.HygieneRule{},
			Truncated: s.truncated,
		}
		for _, rule := range s.rules {
			resp.Rules = append(resp.Rules, rule.HygieneRule)
		}
		_ = json.NewEncoder(w).Encode(resp)
	case "/alert-rules":
		if s.rulesStatus != 0 {
			w.WriteHeader(s.rulesStatus)
			return
		}
		configs := append(AlertConfigResponse{}, s.quiet...)
		for _, rule := range s.rules {
			if rule.Config != nil {
				configs = append(configs, *rule.Config)
			}
		}
		_ = json.NewEncoder(w).Encode(configs)
	default:
		http.NotFound(w, r)
	}
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

func unroutedRules(n int) []hygieneRule {
	rules := make([]hygieneRule, 0, n)
	for i := range n {
		r := healthyRule(fmt.Sprintf("r%02d", i))
		r.Routed = boolp(false)
		r.FireCount = count(int64(i))
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

func TestGetAlertHygiene_RequestsTheWindowEndingNow(t *testing.T) {
	for _, tc := range []struct {
		arg, want string
		span      time.Duration
	}{
		{"", "7d", 7 * testDay},
		{"7d", "7d", 7 * testDay},
		{"30d", "30d", 30 * testDay},
		{" 30D ", "30d", 30 * testDay},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			stub := &hygieneStub{}
			result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{Window: tc.arg})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			from, errFrom := time.Parse(time.RFC3339, stub.from.Load().(string))
			to, errTo := time.Parse(time.RFC3339, stub.to.Load().(string))
			if errFrom != nil || errTo != nil || to.Sub(from) != tc.span {
				t.Errorf("requested %v to %v, want an RFC3339 range of %s", stub.from.Load(), stub.to.Load(), tc.span)
			}
			if since := time.Since(to); since < 0 || since > 2*time.Minute {
				t.Errorf("to = %s, want now", to)
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
			// The byte cap may return fewer; it counts those in omitted_for_size.
			if got := len(out.Rules) + out.OmittedForSize; got != tc.returned || out.Truncated != tc.truncated {
				t.Fatalf("returned %d+%d omitted truncated=%v, want %d truncated=%v", len(out.Rules), out.OmittedForSize, out.Truncated, tc.returned, tc.truncated)
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
	if out.From != testWindowFr.Format(time.RFC3339) || out.To != testWindowTo.Format(time.RFC3339) {
		t.Errorf("from, to = %q, %q, want the reported period", out.From, out.To)
	}
	if out.Coverage.Lifecycle.Status != alerts.HygieneStatusComplete {
		t.Errorf("coverage = %+v, want the upstream coverage", out.Coverage)
	}
	if result.Meta["reference_url"] == nil {
		t.Errorf("missing reference_url meta: %v", result.Meta)
	}
}

// The API caps its list by fire count, so quiet rules may be missing; the result must say so.
func TestGetAlertHygiene_SurfacesUpstreamTruncation(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(strconv.FormatBool(truncated), func(t *testing.T) {
			result, err := callGetAlertHygiene(t, &hygieneStub{rules: unroutedRules(1), truncated: truncated}, GetAlertHygieneArgs{})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeHygieneResult(t, result)
			if out.UpstreamTruncated != truncated || out.Truncated {
				t.Errorf("upstream_truncated=%v truncated=%v, want %v false", out.UpstreamTruncated, out.Truncated, truncated)
			}
		})
	}
}

func TestGetAlertHygiene_NoFlaggedRules(t *testing.T) {
	stub := &hygieneStub{rules: []hygieneRule{healthyRule("ok")}}
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

// Without each rule's configuration the fixes would be guesses, so a failed rules
// fetch fails the call instead of returning proposals without current values.
func TestGetAlertHygiene_RuleConfigurationErrorFails(t *testing.T) {
	stub := &hygieneStub{rules: unroutedRules(1), rulesStatus: http.StatusBadGateway}
	if result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{}); err == nil {
		t.Fatalf("expected an error, got %+v", result)
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

// Each unavailable value skips or weakens a check, so the result counts them instead
// of letting "no noisy rules" hide that the inputs were missing.
func TestGetAlertHygiene_CountsUnavailableValuesOfEnabledRules(t *testing.T) {
	unobserved := healthyRule("unobserved")
	unobserved.FireCount, unobserved.FlapCount, unobserved.AckRate = unavailableCount, unavailableCount, unavailableValue
	unknownRouting := healthyRule("unknown-routing")
	unknownRouting.Routed = nil
	noConfig := healthyRule("no-config")
	noConfig.Config = nil
	disabled := healthyRule("disabled")
	disabled.Config.IsDisabled = boolp(true)
	disabled.Routed = nil

	stub := &hygieneStub{rules: []hygieneRule{unobserved, unknownRouting, noConfig, disabled, healthyRule("ok")}}
	result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := hygieneUnavailable{FireCount: 1, FlapCount: 1, AckRate: 1, Routed: 1, RuleConfig: 1}
	if got := decodeHygieneResult(t, result).Unavailable; got != want {
		t.Errorf("unavailable = %+v, want %+v", got, want)
	}
	const wantJSON = `"unavailable":{"fire_count":1,"flap_count":1,"ack_rate":1,"routed":1,"rule_config":1,"activity":0}`
	if body := utils.GetTextContent(t, result); !strings.Contains(body, wantJSON) {
		t.Errorf("payload does not carry %s:\n%s", wantJSON, body)
	}
}

// limit bounds the count, not the bytes: rules with long names must still fit the
// response cap, dropping the lowest-ranked first.
func TestGetAlertHygiene_CapsResponseBytes(t *testing.T) {
	cases := []struct {
		name       string
		flagged    int
		nameLength int
		wantCut    bool
	}{
		{"default limit fits", defaultHygieneLimit, 10, false},
		{"long names are cut to the cap", maxHygieneLimit, 2000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := unroutedRules(tc.flagged)
			for i := range rules {
				rules[i].RuleName = strings.Repeat("n", tc.nameLength) + rules[i].RuleID
			}
			stub := &hygieneStub{rules: rules}
			result, err := callGetAlertHygiene(t, stub, GetAlertHygieneArgs{Limit: maxHygieneLimit})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			body := utils.GetTextContent(t, result)
			if len(body) > maxHygieneResponseBytes {
				t.Errorf("response is %d bytes, cap %d", len(body), maxHygieneResponseBytes)
			}
			out := decodeHygieneResult(t, result)
			if cut := len(out.Rules) < tc.flagged; cut != tc.wantCut || out.Truncated != tc.wantCut {
				t.Fatalf("returned %d of %d, truncated=%v; want cut=%v", len(out.Rules), tc.flagged, out.Truncated, tc.wantCut)
			}
			if out.OmittedForSize != tc.flagged-len(out.Rules) {
				t.Errorf("omitted_for_size = %d, want %d", out.OmittedForSize, tc.flagged-len(out.Rules))
			}
			// Highest fire count ranks first; the cut keeps the top of the ranking.
			if top := fmt.Sprintf("r%02d", tc.flagged-1); len(out.Rules) == 0 || out.Rules[0].Stats.RuleID != top {
				t.Fatalf("first returned rule is not the top-ranked %s", top)
			}
			// Each long-named rule is a little over 2000 bytes, so a cut response
			// with room for one more dropped a rule it did not need to.
			if tc.wantCut && len(body)+2200 <= maxHygieneResponseBytes {
				t.Errorf("response is %d bytes; another rule would have fit under %d", len(body), maxHygieneResponseBytes)
			}
		})
	}
}

func TestGetAlertHygiene_KeepsTheWorstRulePastTheByteCap(t *testing.T) {
	rules := unroutedRules(2)
	rules[1].RuleName = strings.Repeat("x", maxHygieneResponseBytes)
	result, err := callGetAlertHygiene(t, &hygieneStub{rules: rules}, GetAlertHygieneArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeHygieneResult(t, result)
	if len(out.Rules) != 1 || out.Rules[0].Stats.RuleID != "r01" || out.OmittedForSize != 1 || !out.Truncated {
		t.Fatalf("returned %d rules, omitted_for_size=%d truncated=%v; want only r01", len(out.Rules), out.OmittedForSize, out.Truncated)
	}
}

// Every eval_window proposal must pass update_alert's own payload validation.
func TestGetAlertHygiene_EvalWindowProposalsPassUpdateAlertValidation(t *testing.T) {
	for _, window := range []int64{10, 30, 40, 60} {
		t.Run(strconv.FormatInt(window, 10), func(t *testing.T) {
			rule := healthyRule("synthetic")
			rule.Config.EvalWindow = window
			rule.Config.AlertCondition = "count_true(result) >= " + strconv.FormatInt(window, 10)
			rule.FlapCount = count(63)
			result, err := callGetAlertHygiene(t, &hygieneStub{rules: []hygieneRule{rule}}, GetAlertHygieneArgs{})
			if err != nil {
				t.Fatal(err)
			}
			out := decodeHygieneResult(t, result)
			if len(out.Rules) != 1 || len(out.Rules[0].Findings) != 1 {
				t.Fatalf("unexpected result: %+v", out)
			}
			finding := out.Rules[0].Findings[0]
			for _, fix := range []*hygieneFix{&finding.SuggestedFix, finding.AlternativeFix} {
				if fix == nil || fix.Parameter != "eval_window" {
					continue
				}
				proposed, err := strconv.Atoi(fix.Proposed)
				if err != nil || fix.Proposed == fix.Current {
					t.Fatalf("eval_window proposal is not an applicable change: %+v", fix)
				}
				payload := AlertRulePayload{PrimaryIndicator: "synthetic", ExpressionArgs: map[string]AlertExpressionArg{"synthetic": {ID: "synthetic-kpi"}}, EvalWindow: &proposed}
				if err := validateAlertRulePayload(payload); err != nil {
					t.Fatalf("eval_window %d proposed for %d is rejected by update_alert: %v", proposed, window, err)
				}
			}
		})
	}
}

// A configured rule without an activity row is assessed for staleness only when
// complete history proves its silence; otherwise it is counted, never zero-filled.
func TestGetAlertHygiene_AccountsForQuietConfiguredRules(t *testing.T) {
	quiet := AlertRule{ID: "synthetic", EntityID: "group-1", IsDisabled: boolp(false), CreatedAt: testWindowFr.Add(-testDay).Unix()}

	partial := &alerts.HygieneCoverageSource{
		Status: alerts.HygieneStatusPartial, Reason: "continuity unverified",
		Range: &alerts.HygieneRange{From: testWindowFr, To: testWindowTo},
	}
	result, err := callGetAlertHygiene(t, &hygieneStub{quiet: []AlertRule{quiet}, lifecycle: partial}, GetAlertHygieneArgs{Window: "7d"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeHygieneResult(t, result)
	if out.RulesEvaluated != 0 || out.FlaggedRules != 0 || out.Unavailable != (hygieneUnavailable{Activity: 1}) {
		t.Errorf("partial history: evaluated=%d flagged=%d unavailable=%+v, want the quiet rule counted as unassessed",
			out.RulesEvaluated, out.FlaggedRules, out.Unavailable)
	}

	result, err = callGetAlertHygiene(t, &hygieneStub{quiet: []AlertRule{quiet}}, GetAlertHygieneArgs{Window: "7d"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out = decodeHygieneResult(t, result)
	if out.RulesEvaluated != 1 || len(out.Rules) != 1 || out.Rules[0].Stats.RuleID != "synthetic" {
		t.Fatalf("complete history: %+v, want the quiet rule evaluated and flagged", out)
	}
	if f := out.Rules[0].Findings; len(f) != 1 || f[0].Category != hygieneCategoryStale || !strings.Contains(f[0].Reason, "no alert episode") {
		t.Errorf("findings = %+v, want one stale finding saying no episode was recorded", f)
	}
}
