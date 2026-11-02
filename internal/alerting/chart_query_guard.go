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

// A Discover chart alert may use only the chart's catalog signal, so create_kpi / create_alert must
// not rebuild a described chart's query. Stateless --http gives every request its own session, so
// there only the tool copy routes agents.
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
		for _, metric := range promQLNames(signal.EvalQuery + " " + signal.CanonicalQuery) {
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
	for _, selector := range parsePromQL(query) {
		candidates := selector.names
		if len(candidates) == 0 {
			candidates = slices.Sorted(maps.Keys(metrics))
		}
		for _, metric := range candidates {
			if chart, ok := metrics[metric]; ok && selector.selects(metric) {
				return fmt.Sprintf(chartQueryRefusalFormat, what, metric, chart)
			}
		}
	}
	return ""
}

const promQLString = `"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|` + "`[^`]*`"

var (
	promQLNoise     = regexp.MustCompile(promQLString + `|\{(?:` + promQLString + "|[^}\"'`])*\\}|\\[[^\\]]*\\]")
	promQLMatcher   = regexp.MustCompile(`(` + promQLString + `|[a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!~|!=|=)\s*(` + promQLString + `)|(` + promQLString + `)`)
	promQLLabelSet  = regexp.MustCompile(`\b(?:by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)`)
	promQLIdent     = regexp.MustCompile(`\b[a-zA-Z_:][a-zA-Z0-9_:]*\b\s*\(?`)
	promQLBraceName = regexp.MustCompile(`\b([a-zA-Z_:][a-zA-Z0-9_:]*)\s*$`)
)

var promQLKeywords = map[string]bool{
	"by": true, "without": true, "on": true, "ignoring": true, "group_left": true, "group_right": true,
	"bool": true, "and": true, "or": true, "unless": true, "offset": true, "inf": true, "nan": true,
}

// PromQL ANDs every name restriction within one selector, so they are kept together per selector.
type promQLSelector struct {
	names    []string
	matchers []func(metric string) bool
}

func (s promQLSelector) selects(metric string) bool {
	for _, name := range s.names {
		if name != metric {
			return false
		}
	}
	for _, matches := range s.matchers {
		if !matches(metric) {
			return false
		}
	}
	return true
}

func (s *promQLSelector) addName(name string) {
	if name != "" && !slices.Contains(s.names, name) {
		s.names = append(s.names, name)
	}
}

func parsePromQL(query string) []promQLSelector {
	var selectors []promQLSelector
	var rest strings.Builder
	last := 0
	for _, loc := range promQLNoise.FindAllStringIndex(query, -1) {
		before, token := query[last:loc[0]], query[loc[0]:loc[1]]
		last = loc[1]
		if !strings.HasPrefix(token, "{") {
			rest.WriteString(before + " ")
			continue
		}
		var selector promQLSelector
		if m := promQLBraceName.FindStringSubmatchIndex(before); m != nil && !promQLKeywords[strings.ToLower(before[m[2]:m[3]])] {
			selector.addName(before[m[2]:m[3]])
			before = before[:m[2]]
		}
		rest.WriteString(before + " ")
		for _, m := range promQLMatcher.FindAllStringSubmatch(token, -1) {
			label, op, value, bareName := unquotePromQL(m[1]), m[2], unquotePromQL(m[3]), unquotePromQL(m[4])
			switch {
			case bareName != "":
				selector.addName(bareName)
			case label == "__name__" && op == "=":
				selector.addName(value)
			case label == "__name__":
				selector.matchers = append(selector.matchers, promQLNameMatcher(op, value))
			}
		}
		if len(selector.names) > 0 || len(selector.matchers) > 0 {
			selectors = append(selectors, selector)
		}
	}
	rest.WriteString(query[last:])
	for _, match := range promQLIdent.FindAllString(promQLLabelSet.ReplaceAllString(rest.String(), " "), -1) {
		if name := strings.TrimSpace(match); !strings.HasSuffix(match, "(") && !promQLKeywords[strings.ToLower(name)] {
			selectors = append(selectors, promQLSelector{names: []string{name}})
		}
	}
	return selectors
}

func promQLNames(query string) []string {
	var all promQLSelector
	for _, selector := range parsePromQL(query) {
		for _, name := range selector.names {
			all.addName(name)
		}
	}
	return all.names
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
