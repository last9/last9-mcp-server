package alerting

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"last9-mcp/internal/last9api/alerts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An alert on a Discover chart may use only the chart's catalog signal, so create_alert_from_chart
// takes no query. describe_alert_chart records the metrics its signals query, per MCP session,
// and create_kpi / create_alert refuse a query on them instead of rebuilding the chart by hand.
// Stateless --http gives every request its own session, so there only the tool copy routes agents.
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

// recordDescribedChart remembers the metrics the chart's signals query until the session closes.
// Past maxGuardedSessions open sessions the oldest is evicted, so memory stays bounded.
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
		for _, metric := range parsePromQL(signal.EvalQuery + " " + signal.CanonicalQuery).names {
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

// chartQueryRefusal returns the refusal when query reads a metric of a chart described in this
// session, or "". what names the offending input in the message.
func chartQueryRefusal(session *mcp.ServerSession, what, query string) string {
	chartMetrics.mu.Lock()
	defer chartMetrics.mu.Unlock()
	metrics := chartMetrics.sessions[session]
	selection := parsePromQL(query)
	for _, metric := range selection.names {
		if chart, ok := metrics[metric]; ok {
			return fmt.Sprintf(chartQueryRefusalFormat, what, metric, chart)
		}
	}
	for _, metric := range slices.Sorted(maps.Keys(metrics)) {
		for _, matches := range selection.nameMatchers {
			if matches(metric) {
				return fmt.Sprintf(chartQueryRefusalFormat, what, metric, metrics[metric])
			}
		}
	}
	return ""
}

const promQLString = `"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|` + "`[^`]*`"

var (
	promQLNoise    = regexp.MustCompile(promQLString + `|\{(?:` + promQLString + "|[^}\"'`])*\\}|\\[[^\\]]*\\]")
	promQLMatcher  = regexp.MustCompile(`(` + promQLString + `|[a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!~|!=|=)\s*(` + promQLString + `)|(` + promQLString + `)`)
	promQLLabelSet = regexp.MustCompile(`\b(?:by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)`)
	promQLIdent    = regexp.MustCompile(`\b[a-zA-Z_:][a-zA-Z0-9_:]*\b\s*\(?`)
)

var promQLKeywords = map[string]bool{
	"by": true, "without": true, "on": true, "ignoring": true, "group_left": true, "group_right": true,
	"bool": true, "and": true, "or": true, "unless": true, "offset": true, "inf": true, "nan": true,
}

// promQLSelection is what a PromQL query selects: metric names, plus __name__ matchers that
// select by pattern or by exclusion (=~, !=, !~).
type promQLSelection struct {
	names        []string
	nameMatchers []func(metric string) bool
}

// parsePromQL reads the metrics a query selects: names given in braces (__name__="m" or a bare
// quoted "m"), then identifiers outside strings, braces, ranges and grouping clauses, minus
// function calls.
func parsePromQL(query string) promQLSelection {
	var selection promQLSelection
	addName := func(name string) {
		if name != "" && !slices.Contains(selection.names, name) {
			selection.names = append(selection.names, name)
		}
	}
	query = promQLNoise.ReplaceAllStringFunc(query, func(token string) string {
		if !strings.HasPrefix(token, "{") {
			return " "
		}
		for _, m := range promQLMatcher.FindAllStringSubmatch(token, -1) {
			label, op, value, bareName := unquotePromQL(m[1]), m[2], unquotePromQL(m[3]), unquotePromQL(m[4])
			switch {
			case bareName != "":
				addName(bareName)
			case label == "__name__" && op == "=":
				addName(value)
			case label == "__name__":
				selection.nameMatchers = append(selection.nameMatchers, promQLNameMatcher(op, value))
			}
		}
		return " "
	})
	for _, match := range promQLIdent.FindAllString(promQLLabelSet.ReplaceAllString(query, " "), -1) {
		if name := strings.TrimSpace(match); !strings.HasSuffix(match, "(") && !promQLKeywords[strings.ToLower(name)] {
			addName(name)
		}
	}
	return selection
}

func promQLNameMatcher(op, value string) func(string) bool {
	if op == "!=" {
		return func(metric string) bool { return metric != value }
	}
	pattern, err := regexp.Compile("^(?:" + value + ")$")
	if err != nil {
		// Fail closed: a pattern this can't read might select the chart's metric.
		return func(string) bool { return true }
	}
	if op == "!~" {
		return func(metric string) bool { return !pattern.MatchString(metric) }
	}
	return pattern.MatchString
}

// unquotePromQL returns a PromQL string literal's value; anything else is returned as is.
func unquotePromQL(token string) string {
	if len(token) < 2 {
		return token
	}
	inner := token[1 : len(token)-1]
	switch token[0] {
	case '`':
		return inner
	case '\'':
		token = `"` + strings.ReplaceAll(inner, `\'`, `'`) + `"`
	case '"':
	default:
		return token
	}
	if value, err := strconv.Unquote(token); err == nil {
		return value
	}
	return inner
}
