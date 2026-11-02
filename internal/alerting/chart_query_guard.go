package alerting

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"last9-mcp/internal/last9api/alerts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An alert on a Discover chart may use only the chart's catalog signal, so create_alert_from_chart
// takes no query. describe_alert_chart records the metrics its signals query, per MCP session,
// and create_kpi / create_alert refuse a query on them instead of rebuilding the chart by hand.
const (
	maxGuardedSessions      = 256
	maxGuardedChartMetrics  = 64
	chartQueryRefusalFormat = "refused: %s reads metric %s from the Discover chart %s described in this session; nothing was created. " +
		"An alert on a Discover chart can use only the chart's catalog signal, never a custom query or KPI rebuilt from it: " +
		"create it with create_alert_from_chart and a signal_key from describe_alert_chart, or tell the user the chart's alert does not support a custom query"
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

// recordDescribedChart remembers the metrics the chart's signals query. The oldest session is
// evicted past maxGuardedSessions, so memory stays bounded on a long-lived --http server.
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
	}
	for _, signal := range signals {
		for _, metric := range promQLMetricNames(signal.EvalQuery + " " + signal.CanonicalQuery) {
			if _, seen := metrics[metric]; !seen && len(metrics) < maxGuardedChartMetrics {
				metrics[metric] = chart
			}
		}
	}
}

func hasDescribedChart(session *mcp.ServerSession) bool {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	return len(chartMetrics.sessions[session]) > 0
}

// chartQueryRefusal returns the refusal when query reads a metric of a chart described in this
// session, or "". what names the offending input in the message.
func chartQueryRefusal(session *mcp.ServerSession, what, query string) string {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	metrics := chartMetrics.sessions[session]
	for _, metric := range promQLMetricNames(query) {
		if chart, ok := metrics[metric]; ok {
			return fmt.Sprintf(chartQueryRefusalFormat, what, metric, chart)
		}
	}
	return ""
}

var (
	promQLNoise    = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\{[^}]*\}|\[[^\]]*\]`)
	promQLLabelSet = regexp.MustCompile(`\b(?:by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)`)
	promQLIdent    = regexp.MustCompile(`\b[a-zA-Z_:][a-zA-Z0-9_:]*\b\s*\(?`)
)

var promQLKeywords = map[string]bool{
	"by": true, "without": true, "on": true, "ignoring": true, "group_left": true, "group_right": true,
	"bool": true, "and": true, "or": true, "unless": true, "offset": true, "inf": true, "nan": true,
}

// promQLMetricNames returns the metric names a PromQL query selects, in order of appearance:
// identifiers outside strings, label matchers, ranges and grouping clauses, minus function calls.
func promQLMetricNames(query string) []string {
	query = promQLLabelSet.ReplaceAllString(promQLNoise.ReplaceAllString(query, " "), " ")
	var names []string
	for _, match := range promQLIdent.FindAllString(query, -1) {
		if strings.HasSuffix(match, "(") {
			continue
		}
		name := strings.TrimSpace(match)
		if !promQLKeywords[strings.ToLower(name)] && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
