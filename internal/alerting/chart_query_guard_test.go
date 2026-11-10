package alerting

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/last9api/alerts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMetricNames(t *testing.T) {
	for query, want := range map[string][]string{
		`sum(rate(trace_service_apdex_score{service_name="checkout"}[5m]))`:                   {"trace_service_apdex_score"},
		`sum by (service_name) (rate(http_errors_total[5m])) / sum without (pod) (up)`:        {"http_errors_total", "up"},
		`histogram_quantile(0.99, sum by (le) (rate(x_bucket{job="a"}[5m]))) > 1e3 offset 5m`: {"x_bucket"},
		`a / on (job) group_left (env) b unless c`:                                            {"a", "b", "c"},
		`{__name__="raw", job="a"} + {"quoted"} + {"__name__"='single'}`:                      {"raw", "quoted", "single"},
		`label_replace(up{job="}"}, "dst", "m1", "src", "(.*)") + x{job=` + "`m2`" + `}`:      {"up", "x"},
		`sum(rate(m[5m])) default 0`:                                                          {"m"},
		`rate(a[5m]) keep_metric_names if b ifnot c atan2 d`:                                  {"a", "b", "c", "d"},
		`sum(a) by (job) limit 5 + on (job) group_left (env) prefix "p_" b`:                   {"a", "b"},
	} {
		if got := metricNames(query); !reflect.DeepEqual(got, want) {
			t.Errorf("metricNames(%q) = %v, want %v", query, got, want)
		}
	}
}

// Every way PromQL can select a guarded metric must refuse, including __name__ matchers and
// quoted names inside braces, `or` alternatives and queries the parser rejects; metric names that
// only appear as label values or grouping labels must not.
func TestChartQueryRefusalSelectorShapes(t *testing.T) {
	t.Cleanup(resetDescribedChartMetrics)
	recordDescribedChart(nil, "discover-service/chart-a", []alerts.Signal{{EvalQuery: "sum(rate(m1[5m])) default 0", CanonicalQuery: "job:m2:rate5m"}})
	if refusal := chartQueryRefusal(nil, "query", `{__name__!="x"}`); !strings.Contains(refusal, "reads metric job:m2:rate5m ") {
		t.Errorf("pattern refusal names a keyword, not the chart metric: %s", refusal)
	}
	for _, query := range []string{
		`rate(m1[5m])`, `sum by (job) (m1)`, `m1 offset 5m`, `m1 @ 123`, `job:m2:rate5m`, `other / on() m1`,
		`{__name__="m1"}`, `rate({__name__='m1', job="a"}[5m])`, `{"__name__"="m1"}`, `{"m1"}`, `{"m1", job="a"}`,
		`{job="}", __name__="m1"}`, `{__name__=~"m1|x"}`, `{__name__=~"m.*"}`, `{__name__=~"job:.*"}`,
		`{__name__!="x"}`, `{__name__!~"x.*"}`, `{__name__=~"("}`,
		`m1{__name__!="x"}`, `{__name__=~"m1|x", __name__!="x"}`, `{__name__!="m1"} + {__name__=~"m1"}`,
		"# custom\n{__name__=\"m1\"}", "x # custom\n+ {__name__=~\"m.*\"}", "{job=\"a\" # x\n, __name__=\"m1\"}", `m1{job="#"}`,
		`{__name__="x" or __name__="m1"}`, `{__name__="x" or __name__=~"m.*"}`, `{job="a" or __name__="m1", job="b"}`,
		`sum(`, `m1{`, `x{__name__="m1"}`,
	} {
		if refusal := chartQueryRefusal(nil, "query", query); !strings.Contains(refusal, "nothing was created") {
			t.Errorf("%s was not refused", query)
		}
	}
	for _, query := range []string{
		`{job="m1"}`, `sum by (m1) (other)`, `other and m3 or m4 unless m5`, `{__name__="m10"}`,
		`{__name__=~"m1x|y"}`, `{__name__!~"m.*|job:.*"}`, `label_replace(other, "dst", "m1", "src", "(.*)")`,
		`other{__name__!="m1"}`, `{__name__=~"m1|x", __name__!="m1"}`, `{__name__=~"m.*", __name__!~"m1|job:.*"}`,
		"# m1\nother", "other # {__name__=\"m1\"}", `x{job="# m1"}`, "{__name__=\"x\" # \"m1\"\n}",
		`sum(other) default 0`, `other if m3 ifnot m4 keep_metric_names`,
		`{__name__="x" or __name__="y"}`, `{__name__=~"m.*", __name__!="m1" or __name__="x", __name__!="m1"}`,
	} {
		if refusal := chartQueryRefusal(nil, "query", query); refusal != "" {
			t.Errorf("%s refused: %s", query, refusal)
		}
	}
}

// Writes are recorded so a refusal can be shown to have sent nothing.
func chartGuardServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	return chartGuardServerWithKPIQuery(t, `max(queue_depth{queue="orders"})`)
}

func chartGuardServerWithKPIQuery(t *testing.T, queueDepthQuery string) (*httptest.Server, func() []string) {
	t.Helper()
	t.Cleanup(resetDescribedChartMetrics)
	var mu sync.Mutex
	var writes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), `"describe_chart"`):
			_, _ = w.Write([]byte(`{"signals":[{"key":"apdex","eval_query":"avg(trace_service_apdex_score{service_name=\"checkout\"})","unit":"score","query_kind":"metric"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/entities/entity-1/kpis":
			_, _ = w.Write([]byte(`[{"id":"kpi-1","name":"apdex","definition":{"query":"sum(trace_service_apdex_score{service_name=\"checkout\"})","unit":"score"}},` +
				`{"id":"kpi-2","name":"queue_depth","definition":{"query":` + strconv.Quote(queueDepthQuery) + `,"unit":"count"}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/entities/entity-1/alert-rules":
			_, _ = w.Write([]byte(`{"rules":[]}`))
		case r.Method == http.MethodPost:
			mu.Lock()
			writes = append(writes, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-1","name":"n","definition":{"query":"q","unit":"u"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), writes...) }
}

func describeApdexChart(t *testing.T, server *httptest.Server) {
	t.Helper()
	result, _, err := NewDescribeAlertChartHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
		DescribeAlertChartArgs{Surface: "discover-service", ChartKey: "apdex", ServiceName: "checkout"})
	if err != nil || result.IsError {
		t.Fatalf("describe: result=%+v err=%v", result, err)
	}
}

func assertChartQueryRefusal(t *testing.T, text string) {
	t.Helper()
	for _, want := range []string{"trace_service_apdex_score", "discover-service/apdex", "create_alert_from_chart", "signal_key", "nothing was created"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal missing %q: %s", want, text)
		}
	}
}

func TestCreateKPIRefusesDescribedChartQuery(t *testing.T) {
	server, writes := chartGuardServer(t)
	describeApdexChart(t, server)

	args := validCreateKPIArgs()
	args.Query = `sum(trace_service_apdex_score{service_name="checkout"}) < 0.4`
	result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, args)
	if err != nil || !result.IsError {
		t.Fatalf("want a tool error, got result=%+v err=%v", result, err)
	}
	assertChartQueryRefusal(t, resultText(result))
	if got := writes(); len(got) != 0 {
		t.Fatalf("refused create_kpi still wrote: %v", got)
	}
}

func TestCreateKPIAllowsQueriesNotFromADescribedChart(t *testing.T) {
	server, writes := chartGuardServer(t)
	describeApdexChart(t, server)

	result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, validCreateKPIArgs())
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := writes(); !reflect.DeepEqual(got, []string{"/entities/entity-1/kpis/"}) {
		t.Fatalf("writes = %v", got)
	}
}

func TestCreateAlertRefusesKPIWithDescribedChartQuery(t *testing.T) {
	server, writes := chartGuardServer(t)
	describeApdexChart(t, server)

	name := "mcp-eval-apdex"
	rule := AlertRulePayload{PrimaryIndicator: "apdex", ExpressionArgs: map[string]AlertExpressionArg{"apdex": {ID: "kpi-1"}}, RuleName: &name}
	result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
		CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: rule}})
	if err != nil || !result.IsError {
		t.Fatalf("want a tool error, got result=%+v err=%v", result, err)
	}
	assertChartQueryRefusal(t, resultText(result))
	if got := writes(); len(got) != 0 {
		t.Fatalf("refused create_alert still wrote: %v", got)
	}
}

func TestCreateAlertAllowsKPIsNotFromADescribedChart(t *testing.T) {
	server, writes := chartGuardServer(t)
	describeApdexChart(t, server)

	name := "orders-backlog"
	rule := AlertRulePayload{PrimaryIndicator: "queue_depth", ExpressionArgs: map[string]AlertExpressionArg{"queue_depth": {ID: "kpi-2"}}, RuleName: &name}
	result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
		CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: rule}})
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := writes(); !reflect.DeepEqual(got, []string{"/entities/entity-1/alert-rules"}) {
		t.Fatalf("writes = %v", got)
	}
}

// PromQL ANDs every name restriction in one selector, so these select only queue_depth.
var queueDepthOnlyQueries = []string{
	`queue_depth{__name__!="unrelated"}`,
	`{__name__=~"queue_depth|trace_service_apdex_score",__name__!="trace_service_apdex_score"}`,
}

// Neither a comment before a selector nor an `or` alternative inside one may hide the chart metric.
var disguisedChartQueries = []string{
	"# custom\n{__name__=\"trace_service_apdex_score\"}",
	"# custom\n{__name__=~\"trace_service_apdex_.*\"}",
	`{__name__="queue_depth" or __name__="trace_service_apdex_score"}`,
	`{__name__="queue_depth" or __name__=~"trace_service_apdex_.*"}`,
}

func TestCreateKPIRefusesDisguisedChartQueries(t *testing.T) {
	for _, query := range disguisedChartQueries {
		t.Run(query, func(t *testing.T) {
			server, writes := chartGuardServer(t)
			describeApdexChart(t, server)

			args := validCreateKPIArgs()
			args.Query = query
			result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, args)
			if err != nil || !result.IsError {
				t.Fatalf("want a tool error, got result=%+v err=%v", result, err)
			}
			assertChartQueryRefusal(t, resultText(result))
			if got := writes(); len(got) != 0 {
				t.Fatalf("refused create_kpi still wrote: %v", got)
			}
		})
	}
}

func TestCreateAlertRefusesKPIsWithDisguisedChartQueries(t *testing.T) {
	for _, query := range disguisedChartQueries {
		t.Run(query, func(t *testing.T) {
			server, writes := chartGuardServerWithKPIQuery(t, query)
			describeApdexChart(t, server)

			name := "orders-backlog"
			rule := AlertRulePayload{PrimaryIndicator: "queue_depth", ExpressionArgs: map[string]AlertExpressionArg{"queue_depth": {ID: "kpi-2"}}, RuleName: &name}
			result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: rule}})
			if err != nil || !result.IsError {
				t.Fatalf("want a tool error, got result=%+v err=%v", result, err)
			}
			assertChartQueryRefusal(t, resultText(result))
			if got := writes(); len(got) != 0 {
				t.Fatalf("refused create_alert still wrote: %v", got)
			}
		})
	}
}

func TestCreateKPIAllowsSelectorsExcludingTheChartMetric(t *testing.T) {
	for _, query := range queueDepthOnlyQueries {
		t.Run(query, func(t *testing.T) {
			server, writes := chartGuardServer(t)
			describeApdexChart(t, server)

			args := validCreateKPIArgs()
			args.Query = query
			result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, args)
			if err != nil || result.IsError {
				t.Fatalf("%s: result=%+v err=%v", query, result, err)
			}
			if got := writes(); !reflect.DeepEqual(got, []string{"/entities/entity-1/kpis/"}) {
				t.Fatalf("%s: writes = %v", query, got)
			}
		})
	}
}

func TestCreateAlertAllowsKPIsWithSelectorsExcludingTheChartMetric(t *testing.T) {
	for _, query := range queueDepthOnlyQueries {
		t.Run(query, func(t *testing.T) {
			server, writes := chartGuardServerWithKPIQuery(t, query)
			describeApdexChart(t, server)

			name := "orders-backlog"
			rule := AlertRulePayload{PrimaryIndicator: "queue_depth", ExpressionArgs: map[string]AlertExpressionArg{"queue_depth": {ID: "kpi-2"}}, RuleName: &name}
			result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: rule}})
			if err != nil || result.IsError {
				t.Fatalf("%s: result=%+v err=%v", query, result, err)
			}
			if got := writes(); !reflect.DeepEqual(got, []string{"/entities/entity-1/alert-rules"}) {
				t.Fatalf("%s: writes = %v", query, got)
			}
		})
	}
}

// The KPI lookup runs before the rule-name check, so its failures must read like that check's
// and must block the write.
func TestCreateAlertKPILookupFailureSaysNothingWasCreated(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusBadRequest:          `entity_id "missing" is not an alert group; nothing was created`,
		http.StatusNotFound:            `entity_id "missing" is not an alert group; nothing was created`,
		http.StatusInternalServerError: "Nothing was created.",
		http.StatusServiceUnavailable:  "retry later. Nothing was created.",
		http.StatusGatewayTimeout:      "Nothing was created.",
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Cleanup(resetDescribedChartMetrics)
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet && strings.HasPrefix(r.URL.Path, "/entities/") {
					writes.Add(1)
				}
				if r.Method == http.MethodGet && r.URL.Path == "/entities/missing/kpis" {
					w.WriteHeader(status)
					return
				}
				_, _ = w.Write([]byte(`{"signals":[{"key":"apdex","eval_query":"avg(trace_service_apdex_score)","query_kind":"metric"}]}`))
			}))
			t.Cleanup(server.Close)
			describeApdexChart(t, server)

			// No rule_name, so the KPI lookup is the only read gating the write.
			rule := AlertRulePayload{PrimaryIndicator: "queue_depth", ExpressionArgs: map[string]AlertExpressionArg{"queue_depth": {ID: "kpi-2"}}}
			result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				CreateAlertArgs{EntityID: "missing", AlertRuleMutation: AlertRuleMutation{AlertRule: rule}})
			text := fmt.Sprint(err)
			if result != nil {
				text = resultText(result)
			}
			if !strings.Contains(text, want) {
				t.Errorf("want %q, got result=%+v err=%v", want, result, err)
			}
			if n := writes.Load(); n != 0 {
				t.Fatalf("want no writes after a failed KPI lookup, got %d", n)
			}
		})
	}
}

func chartGuardMCP(t *testing.T, upstream *httptest.Server) (connect func() *mcp.ClientSession, call func(*mcp.ClientSession, string, map[string]any) *mcp.CallToolResult) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0"}, nil)
	cfg := testAlertMutationConfig(upstream.URL)
	mcp.AddTool(server, &mcp.Tool{Name: "describe_alert_chart"}, NewDescribeAlertChartHandler(upstream.Client(), cfg))
	mcp.AddTool(server, &mcp.Tool{Name: "create_kpi"}, NewCreateKPIHandler(upstream.Client(), cfg))
	connect = func() *mcp.ClientSession {
		serverTransport, clientTransport := mcp.NewInMemoryTransports()
		if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
			t.Fatal(err)
		}
		session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(context.Background(), clientTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	call = func(session *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	return connect, call
}

var describeApdexArgs = map[string]any{"surface": "discover-service", "chart_key": "apdex", "service_name": "checkout"}

// A chart described in one MCP session must not block KPI authoring in another.
func TestChartQueryGuardIsPerSession(t *testing.T) {
	upstream, writes := chartGuardServer(t)
	connect, call := chartGuardMCP(t, upstream)
	chartSession, otherSession := connect(), connect()
	if r := call(chartSession, "describe_alert_chart", describeApdexArgs); r.IsError {
		t.Fatalf("describe: %s", resultText(r))
	}
	kpi := map[string]any{"entity_id": "entity-1", "name": "apdex", "query": `sum(trace_service_apdex_score{service_name="checkout"})`, "unit": "score"}
	if r := call(chartSession, "create_kpi", kpi); !r.IsError {
		t.Fatalf("chart session create_kpi was not refused: %s", resultText(r))
	}
	if r := call(otherSession, "create_kpi", kpi); r.IsError {
		t.Fatalf("other session create_kpi refused: %s", resultText(r))
	}
	if got := writes(); !reflect.DeepEqual(got, []string{"/entities/entity-1/kpis/"}) {
		t.Fatalf("writes = %v", got)
	}
}

// A closed session's guard is dropped, so closed sessions (every request under stateless
// --http) never hold the maxGuardedSessions slots that live sessions need.
func TestChartQueryGuardIsDroppedWhenSessionCloses(t *testing.T) {
	upstream, _ := chartGuardServer(t)
	connect, call := chartGuardMCP(t, upstream)
	session := connect()
	if r := call(session, "describe_alert_chart", describeApdexArgs); r.IsError {
		t.Fatalf("describe: %s", resultText(r))
	}
	if got := guardedSessionCount(); got != 1 {
		t.Fatalf("guarded sessions after describe = %d, want 1", got)
	}
	_ = session.Close()
	deadline := time.Now().Add(5 * time.Second)
	for guardedSessionCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("closed session still guarded: %d sessions", guardedSessionCount())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func openServerSessions(t *testing.T, n int) []*mcp.ServerSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0"}, nil)
	sessions := make([]*mcp.ServerSession, n)
	for i := range sessions {
		serverTransport, _ := mcp.NewInMemoryTransports()
		session, err := server.Connect(context.Background(), serverTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		sessions[i] = session
	}
	return sessions
}

// At maxGuardedSessions every session stays guarded; one more evicts only the oldest.
func TestChartQueryGuardEvictsOldestSessionPastCap(t *testing.T) {
	t.Cleanup(resetDescribedChartMetrics)
	sessions := openServerSessions(t, maxGuardedSessions+1)
	signal := []alerts.Signal{{EvalQuery: "m1"}}
	for _, session := range sessions[:maxGuardedSessions] {
		recordDescribedChart(session, "discover-service/chart-a", signal)
	}
	if got := guardedSessionCount(); got != maxGuardedSessions {
		t.Fatalf("guarded sessions at cap = %d, want %d", got, maxGuardedSessions)
	}
	if chartQueryRefusal(sessions[0], "query", "m1") == "" {
		t.Fatal("oldest session unguarded at cap")
	}
	recordDescribedChart(sessions[maxGuardedSessions], "discover-service/chart-a", signal)
	if got := guardedSessionCount(); got != maxGuardedSessions {
		t.Fatalf("guarded sessions past cap = %d, want %d", got, maxGuardedSessions)
	}
	if refusal := chartQueryRefusal(sessions[0], "query", "m1"); refusal != "" {
		t.Fatalf("oldest session still guarded past cap: %s", refusal)
	}
	for _, i := range []int{1, maxGuardedSessions - 1, maxGuardedSessions} {
		if chartQueryRefusal(sessions[i], "query", "m1") == "" {
			t.Errorf("session %d unguarded past cap", i)
		}
	}
}

// A session guards its first maxGuardedChartMetrics metrics, across describes, and no more.
func TestChartQueryGuardCapsMetricsPerSession(t *testing.T) {
	t.Cleanup(resetDescribedChartMetrics)
	metric := func(i int) string { return fmt.Sprintf("m%d", i) }
	var first []string
	for i := range maxGuardedChartMetrics - 1 {
		first = append(first, metric(i))
	}
	recordDescribedChart(nil, "discover-service/chart-a", []alerts.Signal{{EvalQuery: strings.Join(first, " + ")}})
	recordDescribedChart(nil, "discover-service/chart-b", []alerts.Signal{{EvalQuery: metric(maxGuardedChartMetrics-1) + " + " + metric(maxGuardedChartMetrics)}})
	for _, i := range []int{0, maxGuardedChartMetrics - 2, maxGuardedChartMetrics - 1} {
		if chartQueryRefusal(nil, "query", metric(i)) == "" {
			t.Errorf("%s unguarded within the cap", metric(i))
		}
	}
	if refusal := chartQueryRefusal(nil, "query", metric(maxGuardedChartMetrics)); refusal != "" {
		t.Errorf("%s guarded past the cap: %s", metric(maxGuardedChartMetrics), refusal)
	}
}

func guardedSessionCount() int {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	if len(chartMetrics.sessions) != len(chartMetrics.order) {
		panic("chart guard sessions and order disagree")
	}
	return len(chartMetrics.sessions)
}
