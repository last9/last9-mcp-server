package workspace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// hangRelease bounds a hanging route, so a sub-call that ignores its context
// fails the elapsed-time check instead of the whole test run.
const hangRelease = 3 * time.Second

// overviewAPI stands in for the Last9 API. responses maps a route ("POST
// /prom_label_values env") to a body; failing routes answer 500, hanging
// routes block until the caller gives up.
type overviewAPI struct {
	responses map[string]string
	failing   map[string]bool
	hanging   map[string]bool

	mu       sync.Mutex
	requests map[string]*http.Request
	bodies   map[string]map[string]any
}

func (a *overviewAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	route := r.Method + " " + r.URL.Path
	var body map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
		if label, ok := body["label"].(string); ok {
			route += " " + label
		}
	}
	a.mu.Lock()
	a.requests[route], a.bodies[route] = r, body
	a.mu.Unlock()

	switch {
	case a.hanging[route]:
		select {
		case <-r.Context().Done():
		case <-time.After(hangRelease):
		}
	case a.failing[route]:
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	default:
		resp, ok := a.responses[route]
		if !ok {
			http.Error(w, "unexpected route "+route, http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, resp)
	}
}

const (
	routeEnvs     = "POST /prom_label_values env"
	routeServices = "POST /prom_query_instant"
	routeMetrics  = "POST /prom_label_values __name__"
	routeLogs     = "GET /logs/api/v1/labels"
	routeTraces   = "GET /cat/api/search/tags"
	routeGroups   = "POST /entities/list"
	routeAlerts   = "GET /alerts/monitor"
)

var allRoutes = []string{routeEnvs, routeServices, routeMetrics, routeLogs, routeTraces, routeGroups, routeAlerts}

func healthyResponses() map[string]string {
	return map[string]string{
		routeEnvs:     `["staging","prod"]`,
		routeServices: `[{"metric":{"service_name":"api"},"value":[1,"10"]},{"metric":{"service_name":"web"},"value":[1,"5"]},{"metric":{"service_name":"worker"},"value":[1,"0"]},{"metric":{"service_name":""},"value":[1,"9"]},{"metric":{"service_name":"nan-svc"},"value":[1,"NaN"]},{"metric":{"service_name":"api"},"value":[1,"3"]}]`,
		routeMetrics:  `["up"]`,
		routeLogs:     `{"status":"success","data":["service","severity"]}`,
		routeTraces:   `{"scopes":[{"name":"span","tags":["http.method"]}]}`,
		routeGroups:   `[{"entities":[{"id":"g1","name":"A"},{"id":"g2","name":"B"}]}]`,
		routeAlerts:   `{"alert_rules":[{"rule_id":"r1","alerts":[{"state":"firing"},{"state":"resolved"}]},{"rule_id":"r2","alerts":[{"state":"Firing"}]}]}`,
	}
}

func runOverview(t *testing.T, api *overviewAPI, callTimeout time.Duration) map[string]any {
	t.Helper()
	return runOverviewAllowing(t, api, callTimeout, nil)
}

func runOverviewAllowing(t *testing.T, api *overviewAPI, callTimeout time.Duration, allowed toolsets.Set) map[string]any {
	t.Helper()
	api.requests, api.bodies = map[string]*http.Request{}, map[string]map[string]any{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:         srv.URL,
		Region:             "ap-south-1",
		PrometheusReadURL:  "https://prom.example",
		PrometheusUsername: "user",
		PrometheusPassword: "pass",
		TokenManager:       &auth.TokenManager{AccessToken: "mock-token", ExpiresAt: time.Now().Add(time.Hour)},
		ToolFilter:         toolsets.Filter{Tools: allowed},
	}
	opts := overviewOptions{now: fixedClock(time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC)), callTimeout: callTimeout}
	res, _, err := newWorkspaceOverviewHandler(srv.Client(), cfg, opts)(context.Background(), &mcp.CallToolRequest{}, GetWorkspaceOverviewArgs{})
	if err != nil {
		t.Fatalf("a failed sub-call must not fail the tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("result marked as error: %+v", res)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	return out
}

func errorFields(t *testing.T, out map[string]any) []string {
	t.Helper()
	errs, ok := out["errors"].([]any)
	if !ok {
		t.Fatalf("errors must always be an array, got %#v", out["errors"])
	}
	var fields []string
	for _, e := range errs {
		entry := e.(map[string]any)
		if strings.TrimSpace(entry["error"].(string)) == "" {
			t.Errorf("error entry %v has no message", entry)
		}
		fields = append(fields, entry["field"].(string))
	}
	return fields
}

func TestWorkspaceOverviewAllOK(t *testing.T) {
	api := &overviewAPI{responses: healthyResponses()}
	out := runOverview(t, api, time.Second)

	want := map[string]any{
		"current_time":       map[string]any{"now": map[string]any{"iso": "2026-10-16T19:00:00Z", "epoch_seconds": float64(1792177200)}, "timezone": "UTC"},
		"lookback_minutes":   float64(60),
		"environments":       []any{"prod", "staging"},
		"telemetry":          map[string]any{"logs": true, "traces": true, "metrics": true},
		"service_count":      float64(3),
		"alert_group_count":  float64(2),
		"firing_alert_count": float64(2),
		"errors":             []any{},
	}
	assertJSONEqual(t, out, want)

	for _, route := range allRoutes {
		if api.requests[route] == nil {
			t.Fatalf("%s was never called", route)
		}
	}
	if q, _ := api.bodies[routeServices]["query"].(string); q != serviceCountQuery || strings.Contains(q, "domain_attributes_count") ||
		api.bodies[routeServices]["timestamp"] != float64(1792177200) || api.bodies[routeServices]["password"] != "pass" {
		t.Errorf("services body = %v", api.bodies[routeServices])
	}
	for _, route := range []string{routeEnvs} {
		matches, _ := api.bodies[route]["matches"].([]any)
		if len(matches) != 1 || matches[0] != "domain_attributes_count{span_kind='SPAN_KIND_SERVER'}" {
			t.Errorf("%s matches = %v", route, matches)
		}
		if api.bodies[route]["timestamp"] != float64(1792177200) || api.bodies[route]["window"] != float64(3600) {
			t.Errorf("%s window = %v..%v", route, api.bodies[route]["timestamp"], api.bodies[route]["window"])
		}
		if api.bodies[route]["read_url"] != "https://prom.example" || api.bodies[route]["password"] != "pass" {
			t.Errorf("%s does not carry the datasource credentials", route)
		}
	}
	if api.bodies[routeMetrics]["limit"] != float64(1) {
		t.Errorf("metrics probe limit = %v, want 1", api.bodies[routeMetrics]["limit"])
	}
	for _, route := range []string{routeLogs, routeTraces} {
		q := api.requests[route].URL.Query()
		if q.Get("region") != "ap-south-1" || q.Get("start") != "1792173600" || q.Get("end") != "1792177200" {
			t.Errorf("%s query = %v", route, q)
		}
	}
	if q := api.requests[routeAlerts].URL.Query(); q.Get("timestamp") != "1792177200" || q.Get("window") != "900" {
		t.Errorf("alerts monitor query = %v", q)
	}
}

// Toolsets filter the served tools, not backend reads (README, AGENTS.md):
// under a logs-only surface the overview still probes every source.
func TestWorkspaceOverviewProbesEverySourceUnderNarrowToolsets(t *testing.T) {
	allowed, err := toolsets.Parse("logs")
	if err != nil {
		t.Fatal(err)
	}
	api := &overviewAPI{responses: healthyResponses()}
	out := runOverviewAllowing(t, api, time.Second, allowed)
	for _, route := range allRoutes {
		if api.requests[route] == nil {
			t.Errorf("%s was not probed under --toolsets=logs", route)
		}
	}
	if fields := errorFields(t, out); len(fields) != 0 {
		t.Errorf("errors = %v, want none", fields)
	}
}

// The description promises firing_alert_count counts what get_alerts lists:
// instances in state firing over get_alerts' default window. A firing rule
// with no instances, and pending or resolved instances, count for nothing.
func TestWorkspaceOverviewFiringCountMatchesGetAlerts(t *testing.T) {
	const monitor = `{"alert_rules":[
		{"rule_id":"r1","state":"firing","alerts":[{"state":"firing"},{"state":"resolved"},{"state":"pending"}]},
		{"rule_id":"r2","state":"Firing","alerts":[{"state":"FIRING"}]},
		{"rule_id":"r3","state":"firing","alerts":[]},
		{"rule_id":"r4","state":"pending","alerts":[{"state":"pending"}]}]}`
	responses := healthyResponses()
	responses[routeAlerts] = monitor
	api := &overviewAPI{responses: responses}
	out := runOverview(t, api, time.Second)
	overviewWindow := api.requests[routeAlerts].URL.Query().Get("window")

	alertsAPI := &overviewAPI{responses: map[string]string{routeAlerts: monitor}, requests: map[string]*http.Request{}, bodies: map[string]map[string]any{}}
	srv := httptest.NewServer(alertsAPI)
	t.Cleanup(srv.Close)
	cfg := models.Config{APIBaseURL: srv.URL, TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}}
	res, _, err := alerting.NewGetAlertsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, alerting.GetAlertsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	listedFiring := 0
	for _, line := range strings.Split(res.Content[0].(*mcp.TextContent).Text, "\n") {
		// Instance states are indented under "Instance N:"; rule states are not.
		if strings.HasPrefix(line, "      State: ") && strings.EqualFold(strings.TrimPrefix(line, "      State: "), "firing") {
			listedFiring++
		}
	}

	if got := out["firing_alert_count"]; got != float64(listedFiring) || listedFiring != 2 {
		t.Errorf("firing_alert_count = %v, get_alerts lists %d firing instances, want 2 each", got, listedFiring)
	}
	if alertsWindow := alertsAPI.requests[routeAlerts].URL.Query().Get("window"); overviewWindow != alertsWindow {
		t.Errorf("overview window = %s, get_alerts default window = %s", overviewWindow, alertsWindow)
	}
}

func TestWorkspaceOverviewEmptyWorkspace(t *testing.T) {
	api := &overviewAPI{responses: map[string]string{
		routeEnvs:     `[]`,
		routeServices: `[]`,
		routeMetrics:  `[]`,
		routeLogs:     `{"status":"success","data":[]}`,
		routeTraces:   `{"scopes":[{"name":"span","tags":[""]}]}`,
		routeGroups:   `[]`,
		routeAlerts:   `{"alert_rules":[{"rule_id":"r1","alerts":[{"state":"resolved"}]}]}`,
	}}
	out := runOverview(t, api, time.Second)
	assertJSONEqual(t, out["telemetry"], map[string]any{"logs": false, "traces": false, "metrics": false})
	assertJSONEqual(t, out["environments"], []any{})
	for _, field := range []string{"service_count", "alert_group_count", "firing_alert_count"} {
		if out[field] != float64(0) {
			t.Errorf("%s = %v, want 0", field, out[field])
		}
	}
	if fields := errorFields(t, out); len(fields) != 0 {
		t.Errorf("errors = %v, want none", fields)
	}
}

// subCalls maps each overview sub-call's route to the field it fills.
var subCalls = []struct {
	route string
	field string
	path  []string
}{
	{route: routeEnvs, field: "environments", path: []string{"environments"}},
	{route: routeServices, field: "service_count", path: []string{"service_count"}},
	{route: routeMetrics, field: "telemetry.metrics", path: []string{"telemetry", "metrics"}},
	{route: routeLogs, field: "telemetry.logs", path: []string{"telemetry", "logs"}},
	{route: routeTraces, field: "telemetry.traces", path: []string{"telemetry", "traces"}},
	{route: routeGroups, field: "alert_group_count", path: []string{"alert_group_count"}},
	{route: routeAlerts, field: "firing_alert_count", path: []string{"firing_alert_count"}},
}

func TestWorkspaceOverviewOneSubCallFails(t *testing.T) {
	for _, tt := range subCalls {
		t.Run(tt.field, func(t *testing.T) {
			api := &overviewAPI{responses: healthyResponses(), failing: map[string]bool{tt.route: true}}
			out := runOverview(t, api, time.Second)
			if fields := errorFields(t, out); len(fields) != 1 || fields[0] != tt.field {
				t.Fatalf("errors = %v, want only %s", fields, tt.field)
			}
			var v any = out
			for _, key := range tt.path {
				m := v.(map[string]any)
				if _, present := m[key]; !present {
					t.Fatalf("%s must be served as null, not omitted", tt.field)
				}
				v = m[key]
			}
			if v != nil {
				t.Fatalf("%s = %v, want null", tt.field, v)
			}
			if out["service_count"] == nil && tt.field != "service_count" {
				t.Fatalf("an unrelated field went null")
			}
		})
	}
}

// The stub hangs until the client drops the request, so each case also pins
// that its sub-call's HTTP path cancels in flight when its context times out.
func TestWorkspaceOverviewSubCallTimesOut(t *testing.T) {
	for _, tt := range subCalls {
		t.Run(tt.field, func(t *testing.T) {
			api := &overviewAPI{responses: healthyResponses(), hanging: map[string]bool{tt.route: true}}
			start := time.Now()
			out := runOverview(t, api, 100*time.Millisecond)
			if elapsed := time.Since(start); elapsed >= hangRelease {
				t.Fatalf("overview waited %s for a hung sub-call", elapsed)
			}
			if fields := errorFields(t, out); len(fields) != 1 || fields[0] != tt.field {
				t.Fatalf("errors = %v, want only %s", fields, tt.field)
			}
		})
	}
}

func TestWorkspaceOverviewAllSubCallsFail(t *testing.T) {
	failing := map[string]bool{}
	for _, route := range allRoutes {
		failing[route] = true
	}
	out := runOverview(t, &overviewAPI{failing: failing}, time.Second)
	want := []string{"environments", "service_count", "telemetry.logs", "telemetry.traces", "telemetry.metrics", "alert_group_count", "firing_alert_count"}
	got := errorFields(t, out)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("errors = %v, want %v", got, want)
	}
	assertJSONEqual(t, out["telemetry"], map[string]any{"logs": nil, "traces": nil, "metrics": nil})
	for _, field := range []string{"environments", "service_count", "alert_group_count", "firing_alert_count"} {
		if out[field] != nil {
			t.Errorf("%s = %v, want null", field, out[field])
		}
	}
	if out["current_time"] == nil {
		t.Fatal("current_time needs no API call and must survive every sub-call failing")
	}
}
