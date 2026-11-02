package alerting

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sync"

	"last9-mcp/internal/last9api/alerts"

	"github.com/VictoriaMetrics/metricsql"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A Discover chart alert may use only the chart's catalog signal, so create_kpi / create_alert must
// not rebuild a described chart's query. Stateless --http gives every request its own session, so
// there only the tool copy routes agents.
const (
	maxGuardedSessions      = 256
	maxGuardedChartMetrics  = 64
	chartQueryRefusalFormat = "refused: %s reads metric %s from the Discover chart %s described in this session; nothing was created. " +
		"An alert on a Discover chart can use only the chart's catalog signal, never a custom query or KPI rebuilt from it: " +
		"create it with create_alert_from_chart and a signal_key from describe_alert_chart, or tell the user the chart's alert does not support a custom query"
	chartQueryUnparseableFormat = "refused: %s is not a MetricsQL query this server can check against the Discover chart described in this session (%v); nothing was created. " +
		"Fix the query's syntax; for an alert on that chart, use create_alert_from_chart with a signal_key from describe_alert_chart"
)

type describedChartMetrics struct {
	mu       sync.Mutex
	sessions map[*mcp.ServerSession]map[string]string // metric -> "surface/chart_key"
	order    []*mcp.ServerSession
}

var chartMetrics = describedChartMetrics{sessions: map[*mcp.ServerSession]map[string]string{}}

func resetDescribedChartMetrics() {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	chartMetrics.sessions = map[*mcp.ServerSession]map[string]string{}
	chartMetrics.order = nil
}

func requestSession(req *mcp.CallToolRequest) *mcp.ServerSession {
	if req == nil {
		return nil
	}
	return req.Session
}

func recordDescribedChart(session *mcp.ServerSession, chart string, signals []alerts.Signal) {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	metrics, ok := chartMetrics.sessions[session]
	if !ok {
		if len(chartMetrics.order) == maxGuardedSessions {
			delete(chartMetrics.sessions, chartMetrics.order[0])
			chartMetrics.order = chartMetrics.order[1:]
		}
		metrics = map[string]string{}
		chartMetrics.sessions[session] = metrics
		chartMetrics.order = append(chartMetrics.order, session)
		if session != nil {
			go forgetWhenClosed(session)
		}
	}
	for _, signal := range signals {
		for _, metric := range slices.Concat(metricNames(signal.EvalQuery), metricNames(signal.CanonicalQuery)) {
			if _, seen := metrics[metric]; !seen && len(metrics) < maxGuardedChartMetrics {
				metrics[metric] = chart
			}
		}
	}
}

func forgetWhenClosed(session *mcp.ServerSession) {
	_ = session.Wait()
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	delete(chartMetrics.sessions, session)
	chartMetrics.order = slices.DeleteFunc(chartMetrics.order, func(s *mcp.ServerSession) bool { return s == session })
}

func hasDescribedChart(session *mcp.ServerSession) bool {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	return len(chartMetrics.sessions[session]) > 0
}

func chartQueryRefusal(session *mcp.ServerSession, what, query string) string {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	metrics := chartMetrics.sessions[session]
	if len(metrics) == 0 {
		return ""
	}
	selectors, err := parseNameSelectors(query)
	if err != nil {
		// Fail closed: a query the guard can't read might select the chart's metric.
		return fmt.Sprintf(chartQueryUnparseableFormat, what, err)
	}
	for _, metric := range slices.Sorted(maps.Keys(metrics)) {
		for _, selector := range selectors {
			if selector.selects(metric) {
				return fmt.Sprintf(chartQueryRefusalFormat, what, metric, metrics[metric])
			}
		}
	}
	return ""
}

type nameSelector []func(metric string) bool

func (s nameSelector) selects(metric string) bool {
	for _, matches := range s {
		if !matches(metric) {
			return false
		}
	}
	return true
}

// Chart eval_queries are MetricsQL: parse them as the backend does so comments, strings and `or`
// alternatives can't hide a metric name.
func parseNameSelectors(query string) ([]nameSelector, error) {
	var selectors []nameSelector
	err := visitLabelFilters(query, func(filters []metricsql.LabelFilter) {
		var selector nameSelector
		for _, filter := range filters {
			if filter.Label == "__name__" {
				selector = append(selector, nameFilterMatcher(filter))
			}
		}
		if len(selector) > 0 {
			selectors = append(selectors, selector)
		}
	})
	return selectors, err
}

func metricNames(query string) []string {
	var names []string
	_ = visitLabelFilters(query, func(filters []metricsql.LabelFilter) {
		for _, filter := range filters {
			if filter.Label == "__name__" && !filter.IsRegexp && !filter.IsNegative && !slices.Contains(names, filter.Value) {
				names = append(names, filter.Value)
			}
		}
	})
	return names
}

func visitLabelFilters(query string, visit func([]metricsql.LabelFilter)) error {
	expr, err := metricsql.Parse(query)
	if err != nil {
		return err
	}
	metricsql.VisitAll(expr, func(e metricsql.Expr) {
		if metric, ok := e.(*metricsql.MetricExpr); ok {
			for _, filters := range metric.LabelFilterss {
				visit(filters)
			}
		}
	})
	return nil
}

func nameFilterMatcher(filter metricsql.LabelFilter) func(string) bool {
	value, negative := filter.Value, filter.IsNegative
	if !filter.IsRegexp {
		return func(metric string) bool { return (metric == value) != negative }
	}
	pattern, err := regexp.Compile("^(?:" + value + ")$")
	if err != nil {
		// Fail closed: a pattern this can't read might select the chart's metric.
		return func(string) bool { return true }
	}
	return func(metric string) bool { return pattern.MatchString(metric) != negative }
}
