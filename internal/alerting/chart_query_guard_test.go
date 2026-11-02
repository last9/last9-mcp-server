package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"last9-mcp/internal/last9api/alerts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPromQLMetricNames(t *testing.T) {
	for query, want := range map[string][]string{
		`sum(rate(trace_service_apdex_score{service_name="checkout"}[5m]))`:                   {"trace_service_apdex_score"},
		`sum by (service_name) (rate(http_errors_total[5m])) / sum without (pod) (up)`:        {"http_errors_total", "up"},
		`histogram_quantile(0.99, sum by (le) (rate(x_bucket{job="a"}[5m]))) > 1e3 offset 5m`: {"x_bucket"},
		`a / on (job) group_left (env) b unless bool c`:                                       {"a", "b", "c"},
		`{__name__="raw", job="a"} + {"quoted"} + {"__name__"='single'}`:                      {"raw", "quoted", "single"},
		`label_replace(up{job="}"}, "dst", "m1", "src", "(.*)") + x{job=` + "`m2`" + `}`:      {"up", "x"},
	} {
		if got := parsePromQL(query).names; !reflect.DeepEqual(got, want) {
			t.Errorf("parsePromQL(%q).names = %v, want %v", query, got, want)
		}
	}
}

// Every way PromQL can select a guarded metric must refuse, including __name__ matchers and
// quoted names inside braces; metric names that only appear as label values or grouping labels
// must not.
func TestChartQueryRefusalSelectorShapes(t *testing.T) {
	t.Cleanup(resetDescribedChartMetrics)
	recordDescribedChart(nil, "discover-service/chart-a", []alerts.Signal{{EvalQuery: "sum(rate(m1[5m]))", CanonicalQuery: "job:m2:rate5m"}})
	for _, query := range []string{
		`rate(m1[5m])`, `sum by (job) (m1)`, `m1 offset 5m`, `m1 @ 123`, `job:m2:rate5m`, `other / on() m1`,
		`{__name__="m1"}`, `rate({__name__='m1', job="a"}[5m])`, `{"__name__"="m1"}`, `{"m1"}`, `{"m1", job="a"}`,
		`{job="}", __name__="m1"}`, `{__name__=~"m1|x"}`, `{__name__=~"m.*"}`, `{__name__=~"job:.*"}`,
		`{__name__!="x"}`, `{__name__!~"x.*"}`, `{__name__=~"("}`,
	} {
		if refusal := chartQueryRefusal(nil, "query", query); !strings.Contains(refusal, "nothing was created") {
			t.Errorf("%s was not refused", query)
		}
	}
	for _, query := range []string{
		`{job="m1"}`, `sum by (m1) (other)`, `other and bool m3 or m4 unless m5`, `{__name__="m10"}`,
		`{__name__=~"m1x|y"}`, `{__name__!~"m.*|job:.*"}`, `label_replace(other, "dst", "m1", "src", "(.*)")`,
	} {
		if refusal := chartQueryRefusal(nil, "query", query); refusal != "" {
			t.Errorf("%s refused: %s", query, refusal)
		}
	}
}

// chartGuardServer fakes describe_alert_chart (one apdex signal), the group's KPIs and both
// create routes, recording every write so a refusal can be shown to have sent nothing.
func chartGuardServer(t *testing.T) (*httptest.Server, func() []string) {
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
				`{"id":"kpi-2","name":"queue_depth","definition":{"query":"max(queue_depth{queue=\"orders\"})","unit":"count"}}]`))
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

// A chart described in one MCP session must not block KPI authoring in another.
func TestChartQueryGuardIsPerSession(t *testing.T) {
	upstream, writes := chartGuardServer(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0"}, nil)
	cfg := testAlertMutationConfig(upstream.URL)
	mcp.AddTool(server, &mcp.Tool{Name: "describe_alert_chart"}, NewDescribeAlertChartHandler(upstream.Client(), cfg))
	mcp.AddTool(server, &mcp.Tool{Name: "create_kpi"}, NewCreateKPIHandler(upstream.Client(), cfg))
	connect := func() *mcp.ClientSession {
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
	call := func(session *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	chartSession, otherSession := connect(), connect()
	if r := call(chartSession, "describe_alert_chart", map[string]any{"surface": "discover-service", "chart_key": "apdex", "service_name": "checkout"}); r.IsError {
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
