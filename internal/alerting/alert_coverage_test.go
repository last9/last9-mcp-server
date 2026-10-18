package alerting

import (
	"context"
	"encoding/json"
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
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type coverageStub struct {
	status int
	body   string

	mu      sync.Mutex
	queries []url.Values
}

func (s *coverageStub) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
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
		gap      string
		kind     string
		tool     string
		wantArgs map[string]string
	}{
		{`{"kind":"uncovered_signal","entity_id":"ent-1","entity_name":"Checkout","service":"checkout","signal":"latency","detail":"no latency rule","suggested_tool":"create_alert"}`,
			"uncovered_signal", "create_alert", map[string]string{"entity_id": "ent-1", "signal": "latency"}},
		{`{"kind":"all_rules_disabled","entity_id":"ent-2","entity_name":"Payments","detail":"3 rules, all disabled","suggested_tool":"patch_alert"}`,
			"all_rules_disabled", "patch_alert", map[string]string{"entity_id": "ent-2"}},
		{`{"kind":"no_notification_channel","entity_id":"ent-3","entity_name":"Search","suggested_tool":"attach_notification_channel"}`,
			"no_notification_channel", "attach_notification_channel", map[string]string{"entity_id": "ent-3"}},
		{`{"kind":"muted_too_long","entity_id":"ent-4","rule_id":"rule-9","rule_name":"p99 latency","severity":"breach","muted_since":1690000000,"mute_until":0,"suggested_tool":"patch_alert"}`,
			"muted_too_long", "patch_alert", map[string]string{"entity_id": "ent-4", "rule_id": "rule-9"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
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
		{"kind":"muted_too_long","rule_id":"r-2","muted_since":1690000000,"mute_until":0,"suggested_tool":"patch_alert"}],
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
	if gaps[1].MuteUntil != "" {
		t.Errorf("open-ended mute_until = %q, want omitted", gaps[1].MuteUntil)
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
	if out.Groups[0].Count != 2 || signals[0].SuggestedArgs["signal"] != "latency" || signals[1].SuggestedArgs["signal"] != "error_rate" {
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
