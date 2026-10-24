package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const opsSummaryBody = `{"schema_version":2,
"alerts_raised":{"status":"available","count":40},
"acknowledged":{"status":"partial","count":12,"reason":"acks before 2026-10-01 were not recorded"},
"unacknowledged":{"status":"unavailable","reason":"lifecycle ledger not enabled"},
"current_backlog":{"status":"bogus","count":5},
"delivered":{"total":{"status":"available","count":30},"slack":{"status":"available","count":20}},
"top_rules":[{"rule_id":"r1","count":9}],
"coverage":{"lifecycle":{"status":"partial","from":"2026-10-01T00:00:00Z"}}}`

type opsFake struct {
	mu            sync.Mutex
	paths         []string
	summaryBody   map[string]any
	hygieneQuery  string
	summaryStatus int
	summary       string
	hygieneStatus int
	hygiene       string
}

func (f *opsFake) serve(t *testing.T) (*http.Client, models.Config) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/alerts/health_summary":
			_ = json.NewDecoder(r.Body).Decode(&f.summaryBody)
			writeOpsFake(w, f.summaryStatus, f.summary)
		case "/alerts/hygiene":
			_, _ = io.Copy(io.Discard, r.Body)
			f.hygieneQuery = r.URL.RawQuery
			writeOpsFake(w, f.hygieneStatus, f.hygiene)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}
}

func writeOpsFake(w http.ResponseWriter, status int, body string) {
	if status != 0 {
		w.WriteHeader(status)
	}
	_, _ = io.WriteString(w, body)
}

func runOpsSummary(t *testing.T, f *opsFake, args GetAlertOperationsSummaryArgs) (*mcp.CallToolResult, error) {
	t.Helper()
	client, cfg := f.serve(t)
	res, _, err := NewGetAlertOperationsSummaryHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	return res, err
}

func opsText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) != 1 {
		t.Fatalf("result = %+v", res)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func decodeOps(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("unexpected error result: %s", opsText(t, res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(opsText(t, res)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func weekArgs() GetAlertOperationsSummaryArgs {
	return GetAlertOperationsSummaryArgs{StartTimeISO: "2026-10-16T00:00:00Z", EndTimeISO: "2026-10-23T00:00:00Z"}
}

func TestGetAlertOperationsSummaryRejectsInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		mutate func(*GetAlertOperationsSummaryArgs)
		want   string
	}{
		"missing start":    {func(a *GetAlertOperationsSummaryArgs) { a.StartTimeISO = "" }, "start_time_iso is required"},
		"missing end":      {func(a *GetAlertOperationsSummaryArgs) { a.EndTimeISO = " " }, "end_time_iso is required"},
		"bad start":        {func(a *GetAlertOperationsSummaryArgs) { a.StartTimeISO = "2026-10-16" }, `"2026-10-16"`},
		"end before start": {func(a *GetAlertOperationsSummaryArgs) { a.EndTimeISO = "2026-10-15T00:00:00Z" }, "end_time_iso must be after start_time_iso"},
		"empty window":     {func(a *GetAlertOperationsSummaryArgs) { a.EndTimeISO = a.StartTimeISO }, "end_time_iso must be after start_time_iso"},
		"over 31 days":     {func(a *GetAlertOperationsSummaryArgs) { a.EndTimeISO = "2026-11-16T00:00:01Z" }, "at most 31 days"},
		"bad timezone":     {func(a *GetAlertOperationsSummaryArgs) { a.Timezone = "Mars/Olympus" }, `"Mars/Olympus"`},
		"local timezone":   {func(a *GetAlertOperationsSummaryArgs) { a.Timezone = "Local" }, `"Local"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &opsFake{summary: opsSummaryBody}
			args := weekArgs()
			tc.mutate(&args)
			res, err := runOpsSummary(t, f, args)
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError || !strings.Contains(opsText(t, res), tc.want) {
				t.Errorf("result = %s, want error containing %q", opsText(t, res), tc.want)
			}
			if len(f.paths) != 0 {
				t.Errorf("made %d API calls for invalid input", len(f.paths))
			}
		})
	}
}

func TestGetAlertOperationsSummaryAcceptsExactly31Days(t *testing.T) {
	f := &opsFake{summary: opsSummaryBody}
	args := weekArgs()
	args.EndTimeISO = "2026-11-16T00:00:00Z"
	res, err := runOpsSummary(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	decodeOps(t, res)
}

func TestGetAlertOperationsSummaryRequest(t *testing.T) {
	f := &opsFake{summary: opsSummaryBody}
	args := weekArgs()
	args.StartTimeISO = "2026-10-16T05:30:00+05:30"
	args.Services = []string{" checkout ", ""}
	args.Severities = []string{"P1"}
	res, err := runOpsSummary(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOps(t, res)
	b := f.summaryBody
	if b["from"] != float64(1792108800) || b["to"] != float64(1792713600) || b["timezone"] != "UTC" {
		t.Errorf("body = %v", b)
	}
	if fmt.Sprint(b["services"]) != "[checkout]" || fmt.Sprint(b["severities"]) != "[P1]" {
		t.Errorf("filters = %v %v", b["services"], b["severities"])
	}
	if strings.Join(f.paths, ",") != "POST /alerts/health_summary" {
		t.Errorf("paths = %v, hygiene must not be called without include_rules", f.paths)
	}
	if out["rules"] != nil {
		t.Errorf("rules = %v without include_rules", out["rules"])
	}
	w := out["window"].(map[string]any)
	if w["start_time_iso"] != "2026-10-16T00:00:00Z" || w["timezone"] != "UTC" {
		t.Errorf("window = %v", w)
	}
}

func TestGetAlertOperationsSummaryPassesStatusThrough(t *testing.T) {
	f := &opsFake{summary: opsSummaryBody}
	res, err := runOpsSummary(t, f, weekArgs())
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOps(t, res)
	metric := func(path ...string) string {
		var v any = out
		for _, p := range path {
			v = v.(map[string]any)[p]
		}
		raw, _ := json.Marshal(v)
		return string(raw)
	}
	want := map[string]string{
		"alerts_raised":   `{"count":40,"status":"available"}`,
		"acknowledged":    `{"count":12,"reason":"acks before 2026-10-01 were not recorded","status":"partial"}`,
		"unacknowledged":  `{"reason":"lifecycle ledger not enabled","status":"unavailable"}`,
		"current_backlog": `{"reason":"unrecognized status \"bogus\"","status":"unavailable"}`,
	}
	for field, w := range want {
		if got := metric(field); got != w {
			t.Errorf("%s = %s, want %s", field, got, w)
		}
	}
	if got := metric("delivered", "email"); got != `{"reason":"not returned by the API","status":"unavailable"}` {
		t.Errorf("delivered.email = %s", got)
	}
	if got := metric("coverage"); got != `{"lifecycle":{"from":"2026-10-01T00:00:00Z","status":"partial"}}` {
		t.Errorf("coverage = %s", got)
	}
	if got := metric("top_rules"); got != `[{"count":9,"rule_id":"r1"}]` {
		t.Errorf("top_rules = %s", got)
	}
}

func TestGetAlertOperationsSummaryMapsAPIErrors(t *testing.T) {
	cases := map[int]string{
		http.StatusNotFound:            "not available",
		http.StatusBadRequest:          "range too large",
		http.StatusInternalServerError: "HTTP 500",
	}
	for status, want := range cases {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := &opsFake{summaryStatus: status, summary: `{"error":"range too large"}`}
			res, err := runOpsSummary(t, f, weekArgs())
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError || !strings.Contains(opsText(t, res), want) {
				t.Errorf("result = %s, want error containing %q", opsText(t, res), want)
			}
		})
	}
}

func hygieneRule(id string, fires int, flaps float64, routed bool) string {
	return fmt.Sprintf(`{"rule_id":%q,"rule_name":"rule %s","fire_count":%d,"flaps_per_day":%g,"routed":%t}`, id, id, fires, flaps, routed)
}

func TestGetAlertOperationsSummaryIncludeRules(t *testing.T) {
	rules := []string{
		hygieneRule("flappy", 5, flappingMinPerDay, true),
		hygieneRule("calm", 3, flappingMinPerDay-0.01, true),
		hygieneRule("lost", 1, 0, false),
		`{"rule_id":"unknown","fire_count":null,"routed":null}`,
	}
	for i := 0; i < opsRulesLimit; i++ {
		rules = append(rules, hygieneRule(fmt.Sprintf("busy%02d", i), 100+i, 0, true))
	}
	f := &opsFake{summary: opsSummaryBody, hygiene: `{"rules":[` + strings.Join(rules, ",") + `]}`}
	args := weekArgs()
	args.IncludeRules = true
	args.Severities = []string{"P1"}
	res, err := runOpsSummary(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOps(t, res)
	if !strings.Contains(f.hygieneQuery, "severities=P1") || !strings.Contains(f.hygieneQuery, "from=1792108800") {
		t.Errorf("hygiene query = %s", f.hygieneQuery)
	}
	r := out["rules"].(map[string]any)
	ids := func(key string) string {
		var got []string
		for _, row := range r[key].([]any) {
			got = append(got, row.(map[string]any)["rule_id"].(string))
		}
		return strings.Join(got, ",")
	}
	top := r["top_by_fire_count"].([]any)
	if len(top) != opsRulesLimit || top[0].(map[string]any)["rule_id"] != fmt.Sprintf("busy%02d", opsRulesLimit-1) {
		t.Errorf("top_by_fire_count = %v", ids("top_by_fire_count"))
	}
	if ids("flapping") != "flappy" || ids("unrouted") != "lost" {
		t.Errorf("flapping = %s, unrouted = %s", ids("flapping"), ids("unrouted"))
	}
	if r["total_rules"] != float64(len(rules)) || r["flapping_total"] != float64(1) || r["unrouted_total"] != float64(1) {
		t.Errorf("totals = %v %v %v", r["total_rules"], r["flapping_total"], r["unrouted_total"])
	}
	if _, ok := out["errors"]; ok {
		t.Errorf("errors = %v", out["errors"])
	}
}

func TestGetAlertOperationsSummaryKeepsSummaryWhenHygieneFails(t *testing.T) {
	f := &opsFake{summary: opsSummaryBody, hygieneStatus: http.StatusNotFound}
	args := weekArgs()
	args.IncludeRules = true
	res, err := runOpsSummary(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOps(t, res)
	if out["rules"] != nil || out["alerts_raised"] == nil {
		t.Errorf("out = %v", out)
	}
	errs, _ := out["errors"].([]any)
	if len(errs) != 1 || !strings.Contains(fmt.Sprint(errs[0]), "rules") || !strings.Contains(fmt.Sprint(errs[0]), "HTTP 404") {
		t.Errorf("errors = %v", out["errors"])
	}
}
