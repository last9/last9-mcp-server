package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// entities is the alert group lookup body; blank serves coverageGroupEntities.
type coverageStub struct {
	status         int
	body           string
	entities       string
	entitiesStatus int

	mu            sync.Mutex
	queries       []url.Values
	entityLookups int
}

// Scoped tests match service "checkout" in env "prod" as entity ent-1.
const coverageGroupEntities = `[{"entities":[
	{"id":"ENT-1","name":"Checkout","metadata":{"labels":{"service_name":"checkout","env":"prod"}}},
	{"id":"ent-2","name":"Cart","metadata":{"labels":{"service_name":"cart","env":"staging"}}}]}]`

func (s *coverageStub) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if r.URL.Path == constants.EndpointEntitiesList {
		s.serveEntities(w)
		return
	}
	s.mu.Lock()
	s.queries = append(s.queries, r.URL.Query())
	s.mu.Unlock()
	if r.Method != http.MethodGet || r.URL.Path != "/alerts/coverage" {
		http.NotFound(w, r)
		return
	}
	if s.status != 0 {
		w.WriteHeader(s.status)
	}
	_, _ = w.Write([]byte(s.body))
}

func (s *coverageStub) serveEntities(w http.ResponseWriter) {
	s.mu.Lock()
	s.entityLookups++
	s.mu.Unlock()
	if s.entitiesStatus != 0 {
		w.WriteHeader(s.entitiesStatus)
		_, _ = w.Write([]byte(`{"error":"lookup failed https://internal.example"}`))
		return
	}
	body := s.entities
	if body == "" {
		body = coverageGroupEntities
	}
	_, _ = w.Write([]byte(body))
}

func callAlertCoverage(t *testing.T, stub *coverageStub, args GetAlertCoverageArgs) (*mcp.CallToolResult, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL: srv.URL,
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	result, _, err := NewGetAlertCoverageHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	return result, err
}

func decodeAlertCoverage(t *testing.T, result *mcp.CallToolResult) alertCoverageResult {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("result = %+v, want one content block", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content = %T, want text", result.Content[0])
	}
	var out alertCoverageResult
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("decode %q: %v", text.Text, err)
	}
	return out
}

const emptyCoverageBody = `{"generated_at":1700000000,"muted_days":7,"gaps":[],"counts":{"uncovered_signal":0,"all_rules_disabled":0,"no_notification_channel":0,"muted_too_long":0}}`

func TestGetAlertCoverage_QueryBuilding(t *testing.T) {
	for _, tc := range []struct {
		name string
		args GetAlertCoverageArgs
		want url.Values
	}{
		{"omitted", GetAlertCoverageArgs{}, url.Values{"muted_days": {"7"}}},
		{"blank", GetAlertCoverageArgs{Service: " ", Environment: "\t", EntityID: "  "}, url.Values{"muted_days": {"7"}}},
		{"padded", GetAlertCoverageArgs{Service: " checkout ", Environment: " prod\n", EntityID: " ent-1 "},
			url.Values{"service": {"checkout"}, "environment": {"prod"}, "entity_id": {"ent-1"}, "muted_days": {"7"}}},
		{"service only", GetAlertCoverageArgs{Service: "checkout"}, url.Values{"service": {"checkout"}, "muted_days": {"7"}}},
		{"environment only", GetAlertCoverageArgs{Environment: "prod"}, url.Values{"environment": {"prod"}, "muted_days": {"7"}}},
		{"entity only", GetAlertCoverageArgs{EntityID: "ent-1"}, url.Values{"entity_id": {"ent-1"}, "muted_days": {"7"}}},
		{"muted_days minimum", GetAlertCoverageArgs{MutedDays: 1}, url.Values{"muted_days": {"1"}}},
		{"muted_days set", GetAlertCoverageArgs{MutedDays: 30}, url.Values{"muted_days": {"30"}}},
		{"muted_days maximum", GetAlertCoverageArgs{MutedDays: 3650}, url.Values{"muted_days": {"3650"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &coverageStub{body: emptyCoverageBody}
			if _, err := callAlertCoverage(t, stub, tc.args); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if len(stub.queries) != 1 {
				t.Fatalf("requests = %d, want 1", len(stub.queries))
			}
			if got := stub.queries[0].Encode(); got != tc.want.Encode() {
				t.Errorf("query = %q, want %q", got, tc.want.Encode())
			}
		})
	}
}

func TestGetAlertCoverage_RejectsInvalidMutedDays(t *testing.T) {
	for _, days := range []int{-1, -30, 3651} {
		stub := &coverageStub{body: emptyCoverageBody}
		_, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{MutedDays: days})
		if err == nil || !strings.Contains(err.Error(), "muted_days must be between 1 and 3650, got "+strconv.Itoa(days)) {
			t.Errorf("muted_days=%d: err = %v, want a muted_days error", days, err)
		}
		if len(stub.queries) != 0 {
			t.Errorf("muted_days=%d: sent %d requests, want 0", days, len(stub.queries))
		}
	}
}

func TestGetAlertCoverage_RendersEachGapKind(t *testing.T) {
	for _, tc := range []struct {
		name        string
		gap         string
		kind        string
		tool        string
		wantContext [3]string // entity_id, rule_id, signal
		wantArgs    map[string]any
	}{
		{"uncovered signal, new rule", `{"kind":"uncovered_signal","entity_id":"ent-1","entity_name":"Checkout","service":"checkout","signal":"latency","detail":"no latency rule","suggested_tool":"create_alert"}`,
			"uncovered_signal", "create_alert", [3]string{"ent-1", "", "latency"}, map[string]any{"entity_id": "ent-1"}},
		{"uncovered signal, from chart", `{"kind":"uncovered_signal","entity_id":"ent-1","service":"checkout","signal":"latency","suggested_tool":"create_alert_from_chart"}`,
			"uncovered_signal", "create_alert_from_chart", [3]string{"ent-1", "", "latency"}, map[string]any{"service_name": "checkout"}},
		{"all rules disabled", `{"kind":"all_rules_disabled","entity_id":"ent-2","entity_name":"Payments","detail":"3 rules, all disabled","suggested_tool":"patch_alert"}`,
			"all_rules_disabled", "patch_alert", [3]string{"ent-2", "", ""}, map[string]any{"entity_id": "ent-2", "is_disabled": false}},
		{"uncovered signal, disabled rule", `{"kind":"uncovered_signal","entity_id":"ent-1","entity_name":"Checkout","service":"checkout","signal":"error_rate","rule_id":"rule-2","rule_name":"errors","detail":"re-enable it","suggested_tool":"patch_alert"}`,
			"uncovered_signal", "patch_alert", [3]string{"ent-1", "rule-2", "error_rate"}, map[string]any{"entity_id": "ent-1", "id": "rule-2", "is_disabled": false}},
		{"no channel", `{"kind":"no_notification_channel","entity_id":"ent-3","entity_name":"Search","rule_id":"rule-5","rule_name":"5xx","severity":"threat","detail":"no channel routes it","suggested_tool":"attach_notification_channel"}`,
			"no_notification_channel", "attach_notification_channel", [3]string{"ent-3", "rule-5", ""}, map[string]any{"alert_group_id": "ent-3", "severity": "threat"}},
		{"no channel, unknown severity", `{"kind":"no_notification_channel","entity_id":"ent-3","rule_id":"rule-5","severity":"critical","suggested_tool":"attach_notification_channel"}`,
			"no_notification_channel", "attach_notification_channel", [3]string{"ent-3", "rule-5", ""}, map[string]any{"alert_group_id": "ent-3"}},
		{"muted too long", `{"kind":"muted_too_long","entity_id":"ent-4","rule_id":"rule-9","rule_name":"p99 latency","severity":"breach","muted_since":1690000000,"mute_until":-1,"suggested_tool":"patch_alert"}`,
			"muted_too_long", "patch_alert", [3]string{"ent-4", "rule-9", ""}, map[string]any{"entity_id": "ent-4", "id": "rule-9", "mute_until": float64(0)}},
		{"unknown tool", `{"kind":"muted_too_long","entity_id":"ent-4","rule_id":"rule-9","suggested_tool":"future_tool"}`,
			"muted_too_long", "future_tool", [3]string{"ent-4", "rule-9", ""}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &coverageStub{body: `{"generated_at":1700000000,"muted_days":7,"gaps":[` + tc.gap + `],"counts":{"` + tc.kind + `":1}}`}
			result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeAlertCoverage(t, result)
			if out.Complete {
				t.Error("complete = true with a gap present")
			}
			if len(out.Groups) != 1 || out.Groups[0].Kind != tc.kind || out.Groups[0].Count != 1 || len(out.Groups[0].Gaps) != 1 {
				t.Fatalf("groups = %+v", out.Groups)
			}
			gap := out.Groups[0].Gaps[0]
			if gap.SuggestedTool != tc.tool {
				t.Errorf("suggested_tool = %q, want %q", gap.SuggestedTool, tc.tool)
			}
			if got := [3]string{gap.EntityID, gap.RuleID, gap.Signal}; got != tc.wantContext {
				t.Errorf("entity_id, rule_id, signal = %q, want %q", got, tc.wantContext)
			}
			if !maps.Equal(gap.SuggestedArgs, tc.wantArgs) {
				t.Errorf("suggested_args = %v, want %v", gap.SuggestedArgs, tc.wantArgs)
			}
			if out.Counts[tc.kind] != 1 {
				t.Errorf("counts = %v", out.Counts)
			}
		})
	}
}

func TestGetAlertCoverage_MutedGapTimes(t *testing.T) {
	stub := &coverageStub{body: `{"generated_at":1700000000,"muted_days":7,"gaps":[
		{"kind":"muted_too_long","rule_id":"r-1","muted_since":1690000000,"mute_until":1700086400,"suggested_tool":"patch_alert"},
		{"kind":"muted_too_long","rule_id":"r-2","muted_since":1690000000,"mute_until":-1,"suggested_tool":"patch_alert"}],
		"counts":{"muted_too_long":2}}`}
	result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeAlertCoverage(t, result)
	if out.GeneratedAt != "2023-11-14T22:13:20Z" {
		t.Errorf("generated_at = %q", out.GeneratedAt)
	}
	gaps := out.Groups[0].Gaps
	if gaps[0].MutedSince != "2023-07-22T04:26:40Z" || gaps[0].MuteUntil != "2023-11-15T22:13:20Z" {
		t.Errorf("timed mute = %q .. %q", gaps[0].MutedSince, gaps[0].MuteUntil)
	}
	if gaps[1].MutedSince != "2023-07-22T04:26:40Z" || gaps[1].MuteUntil != "" {
		t.Errorf("indefinite mute = %q .. %q, want mute_until omitted", gaps[1].MutedSince, gaps[1].MuteUntil)
	}
}

func TestGetAlertCoverage_GroupsInKindOrder(t *testing.T) {
	stub := &coverageStub{body: `{"generated_at":1,"muted_days":7,"gaps":[
		{"kind":"muted_too_long","rule_id":"r-1"},
		{"kind":"zz_kind","entity_id":"ent-8"},
		{"kind":"future_kind","entity_id":"ent-9"},
		{"kind":"no_notification_channel","entity_id":"ent-3"},
		{"kind":"uncovered_signal","entity_id":"ent-1","signal":"latency"},
		{"kind":"all_rules_disabled","entity_id":"ent-2"},
		{"kind":"uncovered_signal","entity_id":"ent-1","signal":"error_rate"}],
		"counts":{"uncovered_signal":2,"all_rules_disabled":1,"no_notification_channel":1,"muted_too_long":1}}`}
	result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeAlertCoverage(t, result)
	var kinds []string
	for _, g := range out.Groups {
		kinds = append(kinds, g.Kind)
	}
	want := "uncovered_signal,all_rules_disabled,no_notification_channel,muted_too_long,future_kind,zz_kind"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("group order = %v, want %s", kinds, want)
	}
	signals := out.Groups[0].Gaps
	if out.Groups[0].Count != 2 || signals[0].Signal != "latency" || signals[1].Signal != "error_rate" {
		t.Errorf("uncovered_signal group = %+v, want both gaps in API order", out.Groups[0])
	}
}

func TestGetAlertCoverage_EmptyGapsNamesScope(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      GetAlertCoverageArgs
		mutedDays int
		want      []string
	}{
		{"organization", GetAlertCoverageArgs{}, 7, []string{"the whole organization", "7 days"}},
		{"scoped", GetAlertCoverageArgs{Service: " checkout ", Environment: "prod", EntityID: "ent-1", MutedDays: 14}, 14,
			[]string{`service "checkout"`, `environment "prod"`, `entity_id "ent-1"`, "14 days"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(emptyCoverageBody, `"muted_days":7`, `"muted_days":`+strconv.Itoa(tc.mutedDays), 1)
			result, err := callAlertCoverage(t, &coverageStub{body: body}, tc.args)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeAlertCoverage(t, result)
			if !out.Complete || len(out.Groups) != 0 {
				t.Fatalf("complete = %v, groups = %+v; want complete with no groups", out.Complete, out.Groups)
			}
			if !strings.Contains(out.Summary, "complete") {
				t.Errorf("summary = %q, want a complete-coverage statement", out.Summary)
			}
			for _, w := range tc.want {
				if !strings.Contains(out.Summary, w) {
					t.Errorf("summary = %q, missing %q", out.Summary, w)
				}
			}
		})
	}
}

func TestGetAlertCoverage_CountsWithoutGapsIsNotComplete(t *testing.T) {
	stub := &coverageStub{body: `{"generated_at":1,"muted_days":7,"gaps":[],"counts":{"uncovered_signal":3}}`}
	result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeAlertCoverage(t, result)
	if out.Complete {
		t.Fatal("complete = true while counts report gaps")
	}
	if strings.Contains(out.Summary, "is complete") || !strings.Contains(out.Summary, "3") {
		t.Errorf("summary = %q, want the unlisted count and no completeness claim", out.Summary)
	}
}

func TestGetAlertCoverage_SummaryAgreesInNumber(t *testing.T) {
	oneGap := `{"kind":"all_rules_disabled","entity_id":"e-1"}`
	for _, tc := range []struct {
		name string
		body string
		args GetAlertCoverageArgs
		want string
	}{
		{"one muted day", strings.Replace(emptyCoverageBody, `"muted_days":7`, `"muted_days":1`, 1), GetAlertCoverageArgs{MutedDays: 1},
			"rules muted for more than 1 day."},
		{"one listed gap", `{"generated_at":1,"muted_days":7,"gaps":[` + oneGap + `],"counts":{"all_rules_disabled":1}}`, GetAlertCoverageArgs{},
			"1 alert coverage gap for the whole organization."},
		{"two listed gaps", `{"generated_at":1,"muted_days":7,"gaps":[` + oneGap + `,` + oneGap + `],"counts":{"all_rules_disabled":2}}`, GetAlertCoverageArgs{},
			"2 alert coverage gaps for the whole organization."},
		{"one unlisted gap", `{"generated_at":1,"muted_days":7,"gaps":[],"counts":{"uncovered_signal":1}}`, GetAlertCoverageArgs{},
			"The API counted 1 coverage gap for the whole organization but listed none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := callAlertCoverage(t, &coverageStub{body: tc.body}, tc.args)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if out := decodeAlertCoverage(t, result); !strings.Contains(out.Summary, tc.want) {
				t.Errorf("summary = %q, want %q", out.Summary, tc.want)
			}
		})
	}
}

func TestGetAlertCoverage_UpstreamErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			stub := &coverageStub{status: status, body: `{"error":"nope https://internal.example token=s3cret"}`}
			result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{})
			if err == nil {
				t.Fatalf("result = %+v, want an error for HTTP %d", result, status)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(status)) {
				t.Errorf("error = %v, want the status", err)
			}
			for _, leak := range []string{"nope", "internal.example", "s3cret"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error = %v, leaks upstream body %q", err, leak)
				}
			}
		})
	}
}

func TestGetAlertCoverage_MissingReportIsNeverComplete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"empty 200", http.StatusOK, ""},
		{"empty 204", http.StatusNoContent, ""},
		{"null", http.StatusOK, "null"},
		{"invalid JSON", http.StatusOK, "not JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := callAlertCoverage(t, &coverageStub{status: tc.status, body: tc.body}, GetAlertCoverageArgs{})
			if err == nil {
				t.Fatalf("result = %+v, want an error", result)
			}
		})
	}
}

func TestGetAlertCoverage_EndpointMissingSaysCoverageUnchecked(t *testing.T) {
	stub := &coverageStub{status: http.StatusNotFound, body: `404 page not found`}
	result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{})
	if err == nil {
		t.Fatalf("result = %+v, want an error", result)
	}
	for _, want := range []string{"not available on this Last9 deployment", "coverage was not checked", "404"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, missing %q", err, want)
		}
	}
}

// The API returns an all-zero report for a scope matching no alert group, so a typo
// must not read as full coverage.
func TestGetAlertCoverage_UnmatchedScopeIsNotComplete(t *testing.T) {
	for _, tc := range []struct {
		name     string
		entities string
		args     GetAlertCoverageArgs
		scope    string
	}{
		{"unknown service", "", GetAlertCoverageArgs{Service: "mcp-qa-nonexistent-svc"}, `service "mcp-qa-nonexistent-svc"`},
		{"service case variant", "", GetAlertCoverageArgs{Service: "Checkout"}, `service "Checkout"`},
		{"service prefix", "", GetAlertCoverageArgs{Service: "check"}, `service "check"`},
		{"unknown environment", "", GetAlertCoverageArgs{Environment: "production"}, `environment "production"`},
		{"service in another environment", "", GetAlertCoverageArgs{Service: "checkout", Environment: "staging"},
			`service "checkout", environment "staging"`},
		{"unknown entity", "", GetAlertCoverageArgs{EntityID: "00000000-0000-0000-0000-000000000000"},
			`entity_id "00000000-0000-0000-0000-000000000000"`},
		{"entity outside service", "", GetAlertCoverageArgs{Service: "cart", EntityID: "ent-1"}, `service "cart", entity_id "ent-1"`},
		{"organization without alert groups", `[{"entities":[]}]`, GetAlertCoverageArgs{}, "the whole organization"},
		{"null lookup", `null`, GetAlertCoverageArgs{}, "the whole organization"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := callAlertCoverage(t, &coverageStub{body: emptyCoverageBody, entities: tc.entities}, tc.args)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeAlertCoverage(t, result)
			if out.Complete {
				t.Fatalf("complete = true for a scope matching no alert group; summary %q", out.Summary)
			}
			for _, want := range []string{"No alert group", tc.scope, "not confirmed", "get_alert_groups"} {
				if !strings.Contains(out.Summary, want) {
					t.Errorf("summary = %q, missing %q", out.Summary, want)
				}
			}
		})
	}
}

func TestGetAlertCoverage_ScopeLookupFailureIsNotComplete(t *testing.T) {
	stub := &coverageStub{body: emptyCoverageBody, entitiesStatus: http.StatusInternalServerError}
	result, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{Service: "checkout"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	out := decodeAlertCoverage(t, result)
	if out.Complete {
		t.Fatal("complete = true although the alert group lookup failed")
	}
	if !strings.Contains(out.Summary, "not confirmed") || strings.Contains(out.Summary, "internal.example") {
		t.Errorf("summary = %q, want an unconfirmed statement without the upstream body", out.Summary)
	}
}

func TestGetAlertCoverage_CapsListedGapsPerKind(t *testing.T) {
	for _, tc := range []struct {
		n           int
		wantListed  int
		wantOmitted int
	}{
		{maxCoverageGapsPerKind - 1, maxCoverageGapsPerKind - 1, 0},
		{maxCoverageGapsPerKind, maxCoverageGapsPerKind, 0},
		{maxCoverageGapsPerKind + 1, maxCoverageGapsPerKind, 1},
	} {
		t.Run(strconv.Itoa(tc.n), func(t *testing.T) {
			gaps := make([]string, tc.n)
			for i := range gaps {
				gaps[i] = `{"kind":"all_rules_disabled","entity_id":"e-` + strconv.Itoa(i) + `"}`
			}
			body := `{"generated_at":1,"muted_days":7,"gaps":[` + strings.Join(gaps, ",") +
				`],"counts":{"all_rules_disabled":` + strconv.Itoa(tc.n) + `}}`
			result, err := callAlertCoverage(t, &coverageStub{body: body}, GetAlertCoverageArgs{})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			out := decodeAlertCoverage(t, result)
			group := out.Groups[0]
			if group.Count != tc.n || len(group.Gaps) != tc.wantListed || group.Omitted != tc.wantOmitted {
				t.Fatalf("group count=%d listed=%d omitted=%d, want %d/%d/%d",
					group.Count, len(group.Gaps), group.Omitted, tc.n, tc.wantListed, tc.wantOmitted)
			}
			if group.Gaps[0].EntityID != "e-0" {
				t.Errorf("first listed gap = %+v, want the API's first", group.Gaps[0])
			}
			narrow := strings.Contains(out.Summary, "narrow by service, environment or entity_id")
			if narrow != (tc.wantOmitted > 0) {
				t.Errorf("summary = %q, want the narrowing hint only when gaps are omitted", out.Summary)
			}
		})
	}
}

func TestGetAlertCoverage_GapsSkipScopeLookup(t *testing.T) {
	for _, body := range []string{
		`{"generated_at":1,"muted_days":7,"gaps":[{"kind":"all_rules_disabled","entity_id":"e-1"}],"counts":{"all_rules_disabled":1}}`,
		`{"generated_at":1,"muted_days":7,"gaps":[],"counts":{"uncovered_signal":3}}`,
	} {
		stub := &coverageStub{body: body}
		if _, err := callAlertCoverage(t, stub, GetAlertCoverageArgs{Service: "nope"}); err != nil {
			t.Fatalf("handler: %v", err)
		}
		if stub.entityLookups != 0 {
			t.Errorf("body %s: entity lookups = %d, want 0 when the report already lists or counts gaps", body, stub.entityLookups)
		}
	}
}

// maxCoverageResultBytes is the size bound for one get_alert_coverage result.
const maxCoverageResultBytes = 32 << 10

func TestGetAlertCoverage_WholeOrgResultFitsSizeBound(t *testing.T) {
	const perKind = 500
	report := alerts.GetAlertCoverageResponse{GeneratedAt: 1700000000, MutedDays: 7, Counts: map[string]int{}}
	for _, kind := range []string{
		alerts.CoverageGapUncoveredSignal, alerts.CoverageGapAllRulesDisabled,
		alerts.CoverageGapNoNotificationChannel, alerts.CoverageGapMutedTooLong,
	} {
		report.Counts[kind] = perKind
		for i := range perKind {
			// Every field set at a realistic length, so this bounds the worst case per gap.
			report.Gaps = append(report.Gaps, alerts.CoverageGap{
				Kind:          kind,
				EntityID:      fmt.Sprintf("8f14e45f-ceea-467a-9575-%012d", i),
				EntityName:    fmt.Sprintf("payments-gateway-service-%d", i),
				Service:       fmt.Sprintf("payments-gateway-service-%d", i),
				Signal:        "http_server_request_error_rate",
				RuleID:        fmt.Sprintf("c9f0f895-fb98-4b91-99f5-%012d", i),
				RuleName:      "Payments gateway p99 latency above SLO threshold",
				Severity:      "breach",
				MutedSince:    1699000000,
				MuteUntil:     1799000000,
				Detail:        "Rule is muted since the last incident review and has no notification channel attached",
				SuggestedTool: "patch_alert",
			})
		}
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	result, err := callAlertCoverage(t, &coverageStub{body: string(body)}, GetAlertCoverageArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > maxCoverageResultBytes {
		t.Errorf("result = %d bytes, want at most %d", len(text), maxCoverageResultBytes)
	}
}
