package apm

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

	"last9-mcp/internal/constants"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file pins the PromQL-injection fix for the three APM handlers that
// previously interpolated user-controlled service_name / env into
// single-quote-delimited label matchers (service_name='%s', env=~'%s',
// server='%s', client='%s'). The matchers must now use double-quote
// delimiters and wrap every user value in utils.EscapePromQLLabel, matching
// the convention already used by sibling handlers (deviations, service_summary,
// databases, change_events).
//
// These tests are hermetic: they spin up an httptest server that records the
// PromQL the handler renders and return an empty Prometheus vector so the
// handler completes without error, then assert on the captured query text.

// apmCaptureServer is a stub Last9 Prom backend that records every PromQL
// query string the handler sends (both range and instant endpoints) and
// replies with HTTP 200 + an empty Prometheus vector response so the handler
// finishes cleanly.
type apmCaptureServer struct {
	*httptest.Server
	mu       sync.Mutex
	rangeQ   []string
	instantQ []string
}

func newApmCaptureServer(t *testing.T) *apmCaptureServer {
	t.Helper()
	c := &apmCaptureServer{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &payload)
		c.mu.Lock()
		switch r.URL.Path {
		case constants.EndpointPromQuery:
			c.rangeQ = append(c.rangeQ, payload.Query)
		case constants.EndpointPromQueryInstant:
			c.instantQ = append(c.instantQ, payload.Query)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *apmCaptureServer) allQueries() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.rangeQ)+len(c.instantQ))
	out = append(out, c.rangeQ...)
	out = append(out, c.instantQ...)
	return out
}

func (c *apmCaptureServer) rangeQueries() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.rangeQ...)
}

func fixedWindowArgs(start, end time.Time) (string, string) {
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

// runPerfDetailsWithCapture drives NewServicePerformanceDetailsHandler over a
// short single-chunk window and returns every captured query string.
func runPerfDetailsWithCapture(t *testing.T, serviceName, env string) []string {
	t.Helper()
	srv := newApmCaptureServer(t)
	handler := NewServicePerformanceDetailsHandler(srv.Client(), perfDetailsTestConfig(srv.URL))
	end := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	start := end.Add(-10 * time.Minute)
	startISO, endISO := fixedWindowArgs(start, end)
	args := ServicePerformanceDetailsArgs{
		ServiceName:  serviceName,
		StartTimeISO: startISO,
		EndTimeISO:   endISO,
		Env:          env,
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("performance details handler returned error: %v", err)
	}
	return srv.allQueries()
}

// runOpsSummaryWithCapture drives NewServiceOperationsSummaryHandler over a
// short window and returns every captured (instant) query string.
func runOpsSummaryWithCapture(t *testing.T, serviceName, env string) []string {
	t.Helper()
	srv := newApmCaptureServer(t)
	handler := NewServiceOperationsSummaryHandler(srv.Client(), perfDetailsTestConfig(srv.URL))
	end := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	start := end.Add(-2 * time.Minute)
	startISO, endISO := fixedWindowArgs(start, end)
	args := ServiceOperationsSummaryArgs{
		ServiceName:  serviceName,
		StartTimeISO: startISO,
		EndTimeISO:   endISO,
		Env:          env,
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("operations summary handler returned error: %v", err)
	}
	return srv.allQueries()
}

// runDepGraphWithCapture drives NewServiceDependencyGraphHandler over a short
// window and returns every captured (instant) query string.
func runDepGraphWithCapture(t *testing.T, serviceName, env string) []string {
	t.Helper()
	srv := newApmCaptureServer(t)
	handler := NewServiceDependencyGraphHandler(srv.Client(), perfDetailsTestConfig(srv.URL))
	end := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	start := end.Add(-2 * time.Minute)
	startISO, endISO := fixedWindowArgs(start, end)
	args := ServiceDependencyGraphArgs{
		ServiceName:  serviceName,
		StartTimeISO: startISO,
		EndTimeISO:   endISO,
		Env:          env,
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("dependency graph handler returned error: %v", err)
	}
	return srv.allQueries()
}

// assertEveryQueryContains fails the test if any captured query lacks the
// expected substring.
func assertEveryQueryContains(t *testing.T, queries []string, want, label string) {
	t.Helper()
	if len(queries) == 0 {
		t.Fatalf("no queries captured for %s assertion", label)
	}
	for i, q := range queries {
		if !strings.Contains(q, want) {
			t.Errorf("%s: query %d missing %q:\n%s", label, i, want, q)
		}
	}
}

// assertNoQueryContains fails if any captured query contains the forbidden
// substring. Use only with values known NOT to embed the forbidden substring
// themselves (see comment on safeEscapeInputs).
func assertNoQueryContains(t *testing.T, queries []string, forbidden, label string) {
	t.Helper()
	for i, q := range queries {
		if strings.Contains(q, forbidden) {
			t.Errorf("%s: query %d still contains forbidden %q:\n%s", label, i, forbidden, q)
		}
	}
}

// escapeSvcMatcher returns the exact double-quoted, escaped service_name
// matcher substring the handlers must emit.
func escapeSvcMatcher(serviceName string) string {
	return `service_name="` + utils.EscapePromQLLabel(serviceName) + `"`
}

// escapeEnvRegexMatcher returns the exact `env=~"<escaped>"` substring.
func escapeEnvRegexMatcher(env string) string {
	return `env=~"` + utils.EscapePromQLLabel(env) + `"`
}

// escapeEnvExactMatcher returns the exact `env="<escaped>"` substring.
func escapeEnvExactMatcher(env string) string {
	return `env="` + utils.EscapePromQLLabel(env) + `"`
}

// effectiveEnv mirrors the handlers' "" -> ".*" default so assertions compare
// against the value actually rendered into the PromQL.
func effectiveEnv(env string) string {
	if env == "" {
		return ".*"
	}
	return env
}

// everyQueryHasEnvMatcher asserts each query carries the escaped env in either
// the regex (env=~) or exact (env=) double-quoted form — the three handlers use
// both styles across different sub-queries.
func everyQueryHasEnvMatcher(t *testing.T, queries []string, env string) {
	t.Helper()
	eff := effectiveEnv(env)
	wantRE := escapeEnvRegexMatcher(eff)
	wantEq := escapeEnvExactMatcher(eff)
	for i, q := range queries {
		if !strings.Contains(q, wantRE) && !strings.Contains(q, wantEq) {
			t.Errorf("query %d missing escaped env matcher (%q or %q):\n%s", i, wantRE, wantEq, q)
		}
	}
}

// safeEscapeInputs are service_name / env values that DO NOT themselves embed
// any renderer delimiter substring (service_name=', env=', env=~', server=',
// client='), so a negative single-quote-delimiter assertion on the rendered
// query is unambiguous: any hit must come from the renderer, proving a regression.
var safeEscapeInputs = []string{
	"",   // for env -> defaults to .*
	".*", // default env regex
	"prod",
	"^prod$",
	"prod|staging",
	`acme'test`, // a literal single quote must stay inside the matcher
	`foo"bar`,   // double quote must be escaped
	`foo\bar`,   // backslash must be escaped
	"foo\nbar",  // newline must be escaped
	`a\b"c`,     // mix of all three escapables
}

// ---------------------------------------------------------------------------
// get_service_performance_details
// ---------------------------------------------------------------------------

func TestPerformanceDetails_EscapesServiceNameAndEnv(t *testing.T) {
	for _, svc := range safeEscapeInputs {
		if svc == "" {
			continue // performance details requires service_name (tested separately)
		}
		for _, env := range safeEscapeInputs {
			name := "svc=" + svc + "/env=" + env
			t.Run(name, func(t *testing.T) {
				queries := runPerfDetailsWithCapture(t, svc, env)
				if len(queries) == 0 {
					t.Fatalf("no queries captured")
				}
				// Every query must filter on the escaped service_name.
				assertEveryQueryContains(t, queries, escapeSvcMatcher(svc), "service_name")
				// Every query must filter on the escaped env (regex or exact form).
				everyQueryHasEnvMatcher(t, queries, env)
				// No renderer-level single-quote user matcher remains. Safe because
				// none of these inputs embed those substrings.
				assertNoQueryContains(t, queries, `service_name='`, "service_name delimiter")
				assertNoQueryContains(t, queries, `env=~'`, "env=~ delimiter")
				assertNoQueryContains(t, queries, `env='`, "env= delimiter")
			})
		}
	}
}

func TestPerformanceDetails_ContainsInjectionInServiceName(t *testing.T) {
	// The balanced payload from the bug report. Pre-fix this closed the
	// single-quoted matcher early and injected a second apdex sub-query inside
	// sum(...). Post-fix the entire payload is one double-quoted literal and
	// promql-engine-level injection is impossible.
	payload := `api'} or trace_service_apdex_score{service_name='other'} or trace_service_apdex_score{service_name='api`
	queries := runPerfDetailsWithCapture(t, payload, "")
	if len(queries) == 0 {
		t.Fatalf("no queries captured")
	}
	want := escapeSvcMatcher(payload)
	// At least the apdex query (a range sub-query) must carry the full payload
	// verbatim inside one double-quoted literal, proving the injection did not
	// break out of the matcher.
	found := false
	for _, q := range queries {
		if strings.Contains(q, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no query carried contained injection literal %q:\n%v", want, queries)
	}
}

// ---------------------------------------------------------------------------
// get_service_operations_summary
// ---------------------------------------------------------------------------

func TestOperationsSummary_EscapesServiceNameAndEnv(t *testing.T) {
	for _, svc := range safeEscapeInputs {
		if svc == "" {
			continue
		}
		for _, env := range safeEscapeInputs {
			name := "svc=" + svc + "/env=" + env
			t.Run(name, func(t *testing.T) {
				queries := runOpsSummaryWithCapture(t, svc, env)
				if len(queries) == 0 {
					t.Fatalf("no queries captured")
				}
				assertEveryQueryContains(t, queries, escapeSvcMatcher(svc), "service_name")
				everyQueryHasEnvMatcher(t, queries, env)
				assertNoQueryContains(t, queries, `service_name='`, "service_name delimiter")
				assertNoQueryContains(t, queries, `env=~'`, "env=~ delimiter")
				assertNoQueryContains(t, queries, `env='`, "env= delimiter")
			})
		}
	}
}

func TestOperationsSummary_ContainsInjectionInServiceName(t *testing.T) {
	payload := `api'} or trace_endpoint_count{service_name='other'} or trace_endpoint_count{service_name='api`
	queries := runOpsSummaryWithCapture(t, payload, "")
	if len(queries) == 0 {
		t.Fatalf("no queries captured")
	}
	want := escapeSvcMatcher(payload)
	found := false
	for _, q := range queries {
		if strings.Contains(q, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no query carried contained injection literal %q:\n%v", want, queries)
	}
}

// ---------------------------------------------------------------------------
// get_service_dependency_graph
// ---------------------------------------------------------------------------

func assertDepGraphServerClientEscaped(t *testing.T, queries []string, serviceName string) {
	t.Helper()
	wantServer := `server="` + utils.EscapePromQLLabel(serviceName) + `"`
	wantClient := `client="` + utils.EscapePromQLLabel(serviceName) + `"`
	// Incoming queries filter by server="<svc>"; outgoing/infrastructure by
	// client="<svc>". Every captured query must carry one or the other.
	for i, q := range queries {
		if !strings.Contains(q, wantServer) && !strings.Contains(q, wantClient) {
			t.Errorf("query %d missing escaped server/client matcher (%q or %q):\n%s", i, wantServer, wantClient, q)
		}
	}
}

func TestDependencyGraph_EscapesServiceNameAndEnv(t *testing.T) {
	for _, svc := range safeEscapeInputs {
		if svc == "" {
			continue // dependency graph requires service_name
		}
		for _, env := range safeEscapeInputs {
			name := "svc=" + svc + "/env=" + env
			t.Run(name, func(t *testing.T) {
				queries := runDepGraphWithCapture(t, svc, env)
				if len(queries) == 0 {
					t.Fatalf("no queries captured")
				}
				assertDepGraphServerClientEscaped(t, queries, svc)
				everyQueryHasEnvMatcher(t, queries, env)
				assertNoQueryContains(t, queries, `server='`, "server delimiter")
				assertNoQueryContains(t, queries, `client='`, "client delimiter")
				assertNoQueryContains(t, queries, `env=~'`, "env=~ delimiter")
				assertNoQueryContains(t, queries, `env='`, "env= delimiter")
			})
		}
	}
}

func TestDependencyGraph_ContainsInjectionInServiceName(t *testing.T) {
	payload := `api'} or trace_call_graph_count{server='other'} or trace_call_graph_count{server='api`
	queries := runDepGraphWithCapture(t, payload, "")
	if len(queries) == 0 {
		t.Fatalf("no queries captured")
	}
	wantServer := `server="` + utils.EscapePromQLLabel(payload) + `"`
	wantClient := `client="` + utils.EscapePromQLLabel(payload) + `"`
	found := false
	for _, q := range queries {
		if strings.Contains(q, wantServer) || strings.Contains(q, wantClient) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no query carried contained injection literal (%q or %q):\n%v", wantServer, wantClient, queries)
	}
}

// ---------------------------------------------------------------------------
// Happy-path: plain values still produce valid, double-quoted matchers and the
// handlers complete without error.
// ---------------------------------------------------------------------------

func TestPerformanceDetails_HappyPathDoubleQuotedMatchers(t *testing.T) {
	queries := runPerfDetailsWithCapture(t, "checkout", "prod")
	if len(queries) == 0 {
		t.Fatalf("no queries captured")
	}
	for i, q := range queries {
		if !strings.Contains(q, `service_name="checkout"`) {
			t.Errorf("query %d missing service_name=\"checkout\":\n%s", i, q)
		}
		if strings.Contains(q, `service_name='`) {
			t.Errorf("query %d regressed to single-quote matcher:\n%s", i, q)
		}
	}
}

func TestOperationsSummary_HappyPathDoubleQuotedMatchers(t *testing.T) {
	queries := runOpsSummaryWithCapture(t, "checkout", "prod")
	if len(queries) == 0 {
		t.Fatalf("no queries captured")
	}
	for i, q := range queries {
		if !strings.Contains(q, `service_name="checkout"`) {
			t.Errorf("query %d missing service_name=\"checkout\":\n%s", i, q)
		}
		if strings.Contains(q, `service_name='`) {
			t.Errorf("query %d regressed to single-quote matcher:\n%s", i, q)
		}
	}
}

func TestDependencyGraph_HappyPathDoubleQuotedMatchers(t *testing.T) {
	queries := runDepGraphWithCapture(t, "checkout", "prod")
	if len(queries) == 0 {
		t.Fatalf("no queries captured")
	}
	sawServer, sawClient := false, false
	for _, q := range queries {
		if strings.Contains(q, `server="checkout"`) {
			sawServer = true
		}
		if strings.Contains(q, `client="checkout"`) {
			sawClient = true
		}
		if strings.Contains(q, `server='`) || strings.Contains(q, `client='`) {
			t.Errorf("query regressed to single-quote matcher:\n%s", q)
		}
	}
	if !sawServer {
		t.Errorf("no query filtered by server=\"checkout\"")
	}
	if !sawClient {
		t.Errorf("no query filtered by client=\"checkout\"")
	}
}
