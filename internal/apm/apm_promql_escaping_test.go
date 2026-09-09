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
//
// The tests are table-driven over escapingHandlers: adding the next handler
// to the escaping contract is a one-entry change, not a copied test set.

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

// escapingHandler describes one handler under the escaping contract.
type escapingHandler struct {
	name string
	// run drives the handler over a short fixed window with the given
	// service_name / env and returns every captured query string.
	run func(t *testing.T, serviceName, env string) []string
	// svcMatchers returns the double-quoted, escaped matcher substrings for
	// the service value; a query must contain at least one of them.
	svcMatchers func(serviceName string) []string
	// forbiddenDelims are the renderer-level single-quote delimiter prefixes
	// that must never appear in any rendered query.
	forbiddenDelims []string
	// injectionPayload is the balanced breakout payload from the bug report
	// for this handler's metric names. Pre-fix it closed the single-quoted
	// matcher early and injected a second sub-query; post-fix it must be
	// carried verbatim inside one double-quoted literal.
	injectionPayload string
}

// runHandlerWithCapture is the shared driver: it builds the handler via
// construct, runs it over a fixed window ending 2026-01-01T12:00:00Z, and
// returns all captured queries.
func runHandlerWithCapture(
	t *testing.T,
	window time.Duration,
	call func(cfgURL string, client *http.Client, startISO, endISO string) error,
) []string {
	t.Helper()
	srv := newApmCaptureServer(t)
	end := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	start := end.Add(-window)
	if err := call(srv.URL, srv.Client(), start.Format(time.RFC3339), end.Format(time.RFC3339)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return srv.allQueries()
}

var escapingHandlers = []escapingHandler{
	{
		name: "performance_details",
		run: func(t *testing.T, serviceName, env string) []string {
			// 10m keeps the window single-chunk.
			return runHandlerWithCapture(t, 10*time.Minute, func(url string, client *http.Client, startISO, endISO string) error {
				handler := NewServicePerformanceDetailsHandler(client, apmTestConfig(url))
				_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServicePerformanceDetailsArgs{
					ServiceName: serviceName, Env: env, StartTimeISO: startISO, EndTimeISO: endISO,
				})
				return err
			})
		},
		svcMatchers:      func(s string) []string { return []string{`service_name="` + utils.EscapePromQLLabel(s) + `"`} },
		forbiddenDelims:  []string{`service_name='`, `env=~'`, `env='`},
		injectionPayload: `api'} or trace_service_apdex_score{service_name='other'} or trace_service_apdex_score{service_name='api`,
	},
	{
		name: "operations_summary",
		run: func(t *testing.T, serviceName, env string) []string {
			return runHandlerWithCapture(t, 2*time.Minute, func(url string, client *http.Client, startISO, endISO string) error {
				handler := NewServiceOperationsSummaryHandler(client, apmTestConfig(url))
				_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceOperationsSummaryArgs{
					ServiceName: serviceName, Env: env, StartTimeISO: startISO, EndTimeISO: endISO,
				})
				return err
			})
		},
		svcMatchers:      func(s string) []string { return []string{`service_name="` + utils.EscapePromQLLabel(s) + `"`} },
		forbiddenDelims:  []string{`service_name='`, `env=~'`, `env='`},
		injectionPayload: `api'} or trace_endpoint_count{service_name='other'} or trace_endpoint_count{service_name='api`,
	},
	{
		name: "dependency_graph",
		run: func(t *testing.T, serviceName, env string) []string {
			return runHandlerWithCapture(t, 2*time.Minute, func(url string, client *http.Client, startISO, endISO string) error {
				handler := NewServiceDependencyGraphHandler(client, apmTestConfig(url))
				_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceDependencyGraphArgs{
					ServiceName: serviceName, Env: env, StartTimeISO: startISO, EndTimeISO: endISO,
				})
				return err
			})
		},
		// Incoming queries filter by server="<svc>"; outgoing/infrastructure
		// by client="<svc>". Every captured query must carry one or the other.
		svcMatchers: func(s string) []string {
			esc := utils.EscapePromQLLabel(s)
			return []string{`server="` + esc + `"`, `client="` + esc + `"`}
		},
		forbiddenDelims:  []string{`server='`, `client='`, `env=~'`, `env='`},
		injectionPayload: `api'} or trace_call_graph_count{server='other'} or trace_call_graph_count{server='api`,
	},
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

// containsAny reports whether q contains at least one of the wants.
func containsAny(q string, wants []string) bool {
	for _, w := range wants {
		if strings.Contains(q, w) {
			return true
		}
	}
	return false
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

// TestAPMHandlers_EscapeServiceNameAndEnv asserts, for each handler and each
// (service_name, env) input pair, that every rendered query carries the
// escaped double-quoted matchers and never a single-quote user matcher.
func TestAPMHandlers_EscapeServiceNameAndEnv(t *testing.T) {
	for _, h := range escapingHandlers {
		t.Run(h.name, func(t *testing.T) {
			for _, svc := range safeEscapeInputs {
				if svc == "" {
					continue // service_name is required (rejected before any query renders)
				}
				for _, env := range safeEscapeInputs {
					t.Run("svc="+svc+"/env="+env, func(t *testing.T) {
						queries := h.run(t, svc, env)
						if len(queries) == 0 {
							t.Fatalf("no queries captured")
						}
						eff := effectiveEnv(env)
						wantEnv := []string{escapeEnvRegexMatcher(eff), escapeEnvExactMatcher(eff)}
						for i, q := range queries {
							if !containsAny(q, h.svcMatchers(svc)) {
								t.Errorf("query %d missing escaped service matcher (any of %q):\n%s", i, h.svcMatchers(svc), q)
							}
							// The handlers use both env matcher styles across
							// different sub-queries; require one of the two.
							if !containsAny(q, wantEnv) {
								t.Errorf("query %d missing escaped env matcher (any of %q):\n%s", i, wantEnv, q)
							}
							for _, delim := range h.forbiddenDelims {
								if strings.Contains(q, delim) {
									t.Errorf("query %d regressed to single-quote delimiter %q:\n%s", i, delim, q)
								}
							}
						}
					})
				}
			}
		})
	}
}

// TestAPMHandlers_ContainInjectionPayload renders the balanced breakout
// payload from the bug report through each handler and asserts it stays inside
// one double-quoted literal — promql-engine-level injection is impossible.
func TestAPMHandlers_ContainInjectionPayload(t *testing.T) {
	for _, h := range escapingHandlers {
		t.Run(h.name, func(t *testing.T) {
			queries := h.run(t, h.injectionPayload, "")
			if len(queries) == 0 {
				t.Fatalf("no queries captured")
			}
			wants := h.svcMatchers(h.injectionPayload)
			found := false
			for _, q := range queries {
				if containsAny(q, wants) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no query carried contained injection literal (any of %q):\n%v", wants, queries)
			}
		})
	}
}

// TestAPMHandlers_HappyPathDoubleQuotedMatchers asserts plain values still
// produce valid, double-quoted matchers and the handlers complete without
// error. For the dependency graph it additionally checks both the server= and
// client= matcher shapes appear across the query set.
func TestAPMHandlers_HappyPathDoubleQuotedMatchers(t *testing.T) {
	for _, h := range escapingHandlers {
		t.Run(h.name, func(t *testing.T) {
			queries := h.run(t, "checkout", "prod")
			if len(queries) == 0 {
				t.Fatalf("no queries captured")
			}
			wants := h.svcMatchers("checkout")
			seen := make([]bool, len(wants))
			for i, q := range queries {
				if !containsAny(q, wants) {
					t.Errorf("query %d missing escaped service matcher (any of %q):\n%s", i, wants, q)
				}
				for wi, w := range wants {
					if strings.Contains(q, w) {
						seen[wi] = true
					}
				}
				for _, delim := range h.forbiddenDelims {
					if strings.Contains(q, delim) {
						t.Errorf("query %d regressed to single-quote delimiter %q:\n%s", i, delim, q)
					}
				}
			}
			// Every matcher shape (e.g. both server= and client= for the
			// dependency graph) must appear somewhere in the query set.
			for wi, ok := range seen {
				if !ok {
					t.Errorf("no query filtered by %q", wants[wi])
				}
			}
		})
	}
}
