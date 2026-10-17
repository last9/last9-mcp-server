package guardrail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

const hourMs = int64(time.Hour / time.Millisecond)

var (
	scopedPipeline   = []map[string]any{{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "api"}}}}
	unscopedPipeline = []map[string]any{{"type": "filter", "query": map[string]any{"$containsWords": []any{"Body", "timeout"}}}}
)

type stubEstimator struct {
	bytes int64
	err   error
	calls int
}

func (s *stubEstimator) estimate(context.Context, Query) (last9api.QueryEstimate, error) {
	s.calls++
	return last9api.QueryEstimate{Bytes: s.bytes}, s.err
}

func testGuard(mode string, est *stubEstimator) *Guard {
	limits := testLimits
	limits.Mode = mode
	return &Guard{limits: limits, estimate: est.estimate, estimateTimeout: time.Second, sessions: newSessions(time.Now)}
}

func query(pipeline []map[string]any, rng time.Duration) Query {
	end := int64(1_700_000_000_000)
	return Query{Tool: "get_logs", Signal: Logs, Pipeline: pipeline, StartMs: end - rng.Milliseconds(), EndMs: end}
}

func hasHint(v Verdict, reason string) bool {
	for _, h := range v.Hints {
		if strings.HasPrefix(h, "query_guardrail["+reason+"]") {
			return true
		}
	}
	return false
}

// TestCheck_ModeMatrix runs every check at limit-1, limit and limit+1, with and
// without scope, in each mode.
func TestCheck_ModeMatrix(t *testing.T) {
	cap := testLimits.RangeCap
	scope := testLimits.ScopeThresholdBytes
	budget := testLimits.QueryBudgetBytes
	type tc struct {
		name     string
		pipeline []map[string]any
		rng      time.Duration
		bytes    int64
		reason   string // "" when the query is allowed
	}
	cases := []tc{
		{"range cap-1s unscoped", unscopedPipeline, cap - time.Second, 0, ""},
		{"range at cap unscoped", unscopedPipeline, cap, 0, ""},
		{"range cap+1s unscoped", unscopedPipeline, cap + time.Second, 0, ReasonRangeTooWide},
		{"range cap+1s scoped", scopedPipeline, cap + time.Second, 0, ""},
		{"scope threshold-1 unscoped", unscopedPipeline, time.Hour, scope - 1, ""},
		{"scope at threshold unscoped", unscopedPipeline, time.Hour, scope, ""},
		{"scope threshold+1 unscoped", unscopedPipeline, time.Hour, scope + 1, ReasonUnscopedQuery},
		{"scope threshold+1 scoped", scopedPipeline, time.Hour, scope + 1, ""},
		{"budget-1 scoped", scopedPipeline, time.Hour, budget - 1, ""},
		{"at budget scoped", scopedPipeline, time.Hour, budget, ""},
		{"budget+1 scoped", scopedPipeline, time.Hour, budget + 1, ReasonScanBudgetExceeded},
		{"budget+1 unscoped", unscopedPipeline, time.Hour, budget + 1, ReasonUnscopedQuery},
	}
	for _, mode := range []string{ModeOff, ModeShadow, ModeEnforce} {
		for _, c := range cases {
			t.Run(mode+"/"+c.name, func(t *testing.T) {
				est := &stubEstimator{bytes: c.bytes}
				v := testGuard(mode, est).Check(context.Background(), nil, query(c.pipeline, c.rng))

				if mode == ModeOff {
					if v.Blocked || len(v.Hints) != 0 || est.calls != 0 {
						t.Fatalf("off: blocked=%v hints=%v estimate calls=%d", v.Blocked, v.Hints, est.calls)
					}
					return
				}
				if c.reason == "" {
					if v.Blocked || v.Decision.Action != Allow || len(v.Hints) != 0 {
						t.Fatalf("want allow without hints, got %+v", v)
					}
					return
				}
				if v.Decision.Reason != c.reason {
					t.Fatalf("reason = %q, want %q", v.Decision.Reason, c.reason)
				}
				if want := mode == ModeEnforce; v.Blocked != want {
					t.Fatalf("blocked = %v, want %v", v.Blocked, want)
				}
				if !hasHint(v, c.reason) {
					t.Fatalf("hints %v lack %s", v.Hints, c.reason)
				}
				if mode == ModeShadow && !strings.Contains(v.Hints[0], "would be rejected in enforce mode") {
					t.Errorf("shadow hint %q does not say the query ran unblocked", v.Hints[0])
				}
			})
		}
	}
}

func TestCheck_EstimateUnavailableKeepsRangeCheck(t *testing.T) {
	for _, mode := range []string{ModeShadow, ModeEnforce} {
		t.Run(mode, func(t *testing.T) {
			g := testGuard(mode, &stubEstimator{err: errors.New("boom")})

			v := g.Check(context.Background(), nil, query(unscopedPipeline, time.Hour))
			if v.Blocked || v.Decision.Action != Allow || !hasHint(v, ReasonEstimateUnavailable) {
				t.Fatalf("narrow unscoped query without estimate: %+v", v)
			}

			v = g.Check(context.Background(), nil, query(unscopedPipeline, testLimits.RangeCap+time.Second))
			if v.Decision.Reason != ReasonRangeTooWide || v.Blocked != (mode == ModeEnforce) {
				t.Fatalf("wide unscoped query without estimate: %+v", v)
			}
			if !hasHint(v, ReasonRangeTooWide) || !hasHint(v, ReasonEstimateUnavailable) {
				t.Errorf("hints = %v", v.Hints)
			}
		})
	}
}

func TestCheck_NegativeEstimateIsUnavailable(t *testing.T) {
	v := testGuard(ModeEnforce, &stubEstimator{bytes: -1}).Check(context.Background(), nil, query(scopedPipeline, time.Hour))
	if !hasHint(v, ReasonEstimateUnavailable) {
		t.Fatalf("hints = %v", v.Hints)
	}
}

func serverScopedGuard(bytes int64) *Guard {
	g := testGuard(ModeEnforce, nil)
	g.estimate = func(context.Context, Query) (last9api.QueryEstimate, error) {
		return last9api.QueryEstimate{Bytes: bytes, Scoped: true}, nil
	}
	return g
}

func TestCheck_ServerScopeWaivesScopeThreshold(t *testing.T) {
	v := serverScopedGuard(testLimits.ScopeThresholdBytes+1).Check(context.Background(), nil, query(unscopedPipeline, time.Hour))
	if v.Blocked || v.Decision.Action != Allow {
		t.Fatalf("a query the estimate reports scoped was rejected: %+v", v)
	}
}

// The range cap is the one limit that holds when bytes are low or budgets
// loose, so only a filter detected in the pipeline itself waives it.
func TestCheck_ServerScopeDoesNotWaiveRangeCap(t *testing.T) {
	v := serverScopedGuard(1).Check(context.Background(), nil, query(unscopedPipeline, testLimits.RangeCap+time.Second))
	if !v.Blocked || v.Decision.Reason != ReasonRangeTooWide {
		t.Fatalf("an over-cap query the estimate alone reports scoped: %+v", v)
	}
}

// httpGuard drives the real estimate call against a stub API.
func httpGuard(t *testing.T, mode string, h http.HandlerFunc) *Guard {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL: srv.URL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
		QueryGuardrail: testLimits,
	}
	cfg.QueryGuardrail.Mode = mode
	g := New(srv.Client(), cfg)
	g.sessions = newSessions(time.Now)
	return g
}

func TestCheck_EstimateHTTPFailures(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"400", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			http.Error(w, `{"error":"bad pipeline"}`, http.StatusBadRequest)
		}},
		{"404 before the route ships", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			http.NotFound(w, r)
		}},
		{"500", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusInternalServerError)
		}},
		{"timeout", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := httpGuard(t, ModeEnforce, tt.handler)
			g.estimateTimeout = 50 * time.Millisecond
			start := time.Now()
			v := g.Check(context.Background(), nil, query(scopedPipeline, time.Hour))
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("Check took %s; the estimate timeout did not bound it", elapsed)
			}
			if v.Blocked || !hasHint(v, ReasonEstimateUnavailable) {
				t.Fatalf("verdict = %+v", v)
			}
		})
	}
}

func TestCheck_EstimateHTTPSuccessRejectsOverBudget(t *testing.T) {
	var path string
	g := httpGuard(t, ModeEnforce, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"data":{"bytes":5001,"scoped":true}}`))
	})
	q := query(scopedPipeline, time.Hour)
	q.Signal = Traces
	v := g.Check(context.Background(), nil, q)
	if !v.Blocked || v.Decision.Reason != ReasonScanBudgetExceeded {
		t.Fatalf("verdict = %+v", v)
	}
	if path != "/cat/api/traces/v2/query_range/json/estimate" {
		t.Errorf("estimate path = %q", path)
	}
}

func TestCheck_SessionBudget(t *testing.T) {
	budget := testLimits.SessionBudgetBytes
	tests := []struct {
		name string
		used int64
		warn bool
	}{
		{"79%", budget * 79 / 100, false},
		{"80%", budget * 80 / 100, true},
		{"100%", budget, true},
	}
	for _, mode := range []string{ModeShadow, ModeEnforce} {
		for _, tt := range tests {
			t.Run(mode+"/"+tt.name, func(t *testing.T) {
				// Two queries split the usage, so the warning depends on the running total.
				first := tt.used / 2
				est := &stubEstimator{bytes: first}
				g := testGuard(mode, est)
				g.limits.QueryBudgetBytes = budget
				g.limits.ScopeThresholdBytes = budget

				if v := g.Check(context.Background(), nil, query(scopedPipeline, time.Hour)); v.Decision.Action != Allow {
					t.Fatalf("first query: %+v", v)
				}
				est.bytes = tt.used - first
				v := g.Check(context.Background(), nil, query(scopedPipeline, time.Hour))
				if got := hasHint(v, ReasonSessionBudget80); got != tt.warn {
					t.Fatalf("session warning = %v, want %v (hints %v)", got, tt.warn, v.Hints)
				}
				if v.Blocked {
					t.Fatal("a session warning must not block")
				}
			})
		}
	}
}

func TestCheck_EnforceRejectDoesNotSpendSessionBudget(t *testing.T) {
	est := &stubEstimator{bytes: testLimits.QueryBudgetBytes + 1}
	g := testGuard(ModeEnforce, est)
	g.Check(context.Background(), nil, query(scopedPipeline, time.Hour))
	if got := g.sessions.add(stdioSessionKey, 0); got != 0 {
		t.Fatalf("session usage after a blocked query = %d, want 0", got)
	}

	g = testGuard(ModeShadow, est)
	g.Check(context.Background(), nil, query(scopedPipeline, time.Hour))
	if got := g.sessions.add(stdioSessionKey, 0); got != est.bytes {
		t.Fatalf("session usage after a shadow query that ran = %d, want %d", got, est.bytes)
	}
}

func httpRequest(sessionID string) *mcp.CallToolRequest {
	h := http.Header{}
	if sessionID != "" {
		h.Set("Mcp-Session-Id", sessionID)
	}
	return &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: h}}
}

func TestSessionKey(t *testing.T) {
	tests := []struct {
		name string
		req  *mcp.CallToolRequest
		key  string
		ok   bool
	}{
		{"nil request", nil, stdioSessionKey, true},
		{"stdio", &mcp.CallToolRequest{}, stdioSessionKey, true},
		{"http with session", httpRequest("abc"), "abc", true},
		{"http without session", httpRequest(""), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := sessionKey(tt.req)
			if key != tt.key || ok != tt.ok {
				t.Fatalf("sessionKey = %q, %v; want %q, %v", key, ok, tt.key, tt.ok)
			}
		})
	}
}

// sessionKey reads a nil Extra as stdio. That is safe only while streamable
// HTTP, the server's one HTTP transport, sets Extra.Header on every call.
func TestSessionKey_StreamableHTTPNeverFallsBackToStdio(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "guardrail-test"}, nil)
	keys := make(chan string, 1)
	mcp.AddTool(server, &mcp.Tool{Name: "probe"}, func(_ context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		key, ok := sessionKey(req)
		if !ok {
			key = "<untracked>"
		}
		keys <- key
		return &mcp.CallToolResult{}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "guardrail-test-client"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "probe"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if key := <-keys; key == stdioSessionKey {
		t.Fatal("an HTTP call was keyed as the shared stdio session")
	}
}

func TestCheck_SessionsAreKeyedByHeader(t *testing.T) {
	est := &stubEstimator{bytes: testLimits.SessionBudgetBytes * 8 / 10}
	g := testGuard(ModeEnforce, est)
	g.limits.QueryBudgetBytes = testLimits.SessionBudgetBytes
	g.limits.ScopeThresholdBytes = testLimits.SessionBudgetBytes

	if v := g.Check(context.Background(), httpRequest("a"), query(scopedPipeline, time.Hour)); !hasHint(v, ReasonSessionBudget80) {
		t.Fatalf("session a at 80%%: hints %v", v.Hints)
	}
	est.bytes = 1
	if v := g.Check(context.Background(), httpRequest("b"), query(scopedPipeline, time.Hour)); hasHint(v, ReasonSessionBudget80) {
		t.Fatalf("session b inherited session a's usage: hints %v", v.Hints)
	}
	est.bytes = testLimits.SessionBudgetBytes
	if v := g.Check(context.Background(), httpRequest(""), query(scopedPipeline, time.Hour)); hasHint(v, ReasonSessionBudget80) {
		t.Fatalf("an HTTP call without a session was tracked: hints %v", v.Hints)
	}
}

func TestSessions_ExpireAfterTTL(t *testing.T) {
	now := time.Unix(0, 0)
	s := newSessions(func() time.Time { return now })
	s.add("a", 100)
	s.add("b", 1)

	now = now.Add(sessionTTL)
	if got := s.add("a", 1); got != 101 {
		t.Fatalf("usage at exactly the TTL = %d, want 101", got)
	}
	now = now.Add(sessionTTL + time.Second)
	if got := s.add("a", 1); got != 1 {
		t.Fatalf("usage after the TTL = %d, want 1 (reset)", got)
	}
	if _, ok := s.usage["b"]; ok {
		t.Error("idle session b was not evicted")
	}
}

func TestSessions_EvictIdlePeersWhileActiveKeyStaysHot(t *testing.T) {
	now := time.Unix(0, 0)
	s := newSessions(func() time.Time { return now })
	s.add("sticky", 1)
	s.add("idle", 1)

	now = now.Add(sessionTTL / 2)
	s.add("sticky", 1)
	now = now.Add(sessionTTL/2 + time.Second)
	if got := s.add("sticky", 1); got != 3 {
		t.Fatalf("sticky usage = %d, want 3 (still live)", got)
	}
	if _, ok := s.usage["idle"]; ok {
		t.Fatal("idle session survived while another session stayed active")
	}
}

func TestSessions_CapDropsLeastRecentlySeen(t *testing.T) {
	now := time.Unix(0, 0)
	s := newSessions(func() time.Time { return now })
	s.add("kept", 7)
	for i := range maxSessions + 1000 {
		now = now.Add(time.Millisecond)
		s.add(fmt.Sprintf("minted-%d", i), 1)
		if i%1000 == 0 {
			s.add("kept", 0)
		}
	}
	if got := len(s.usage); got != maxSessions {
		t.Fatalf("sessions tracked = %d, want the cap %d", got, maxSessions)
	}
	if _, ok := s.usage["minted-0"]; ok {
		t.Error("least recently seen session survived past the cap")
	}
	if got := s.add("kept", 0); got != 7 {
		t.Errorf("recently seen session usage = %d, want 7 (kept under the cap)", got)
	}
}

func TestRejectionResult(t *testing.T) {
	v := testGuard(ModeEnforce, &stubEstimator{}).Check(context.Background(), nil, query(unscopedPipeline, 48*time.Hour))
	r := v.RejectionResult()
	if !r.IsError {
		t.Fatal("rejection must be a tool error")
	}
	text := r.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, ReasonRangeTooWide) || !strings.Contains(text, "24h") {
		t.Errorf("text = %q", text)
	}
	envelope := r.Content[1].(*mcp.TextContent).Text
	if !strings.Contains(envelope, "query_guardrail["+ReasonRangeTooWide+"] rejected") {
		t.Errorf("envelope = %q", envelope)
	}
}
