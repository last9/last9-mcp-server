package apm

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// GoldenSignalsQuery scopes QueryGoldenSignals. Env is an exact environment;
// empty means every environment.
type GoldenSignalsQuery struct {
	Services []string
	Env      string
	Window   TimeWindow
}

// GoldenSignals are one service's server-span aggregates over one window.
// Errors are 5xx plus gRPC error spans, the error classes get_service_summary
// counts. Latencies are nil when the service reports no such quantile.
type GoldenSignals struct {
	RequestCount  float64  `json:"request_count"`
	ErrorCount    float64  `json:"error_count"`
	ThroughputRPM float64  `json:"throughput_rpm"`
	ErrorRatePct  *float64 `json:"error_rate_pct"`
	LatencyP50Ms  *float64 `json:"latency_p50_ms"`
	LatencyP95Ms  *float64 `json:"latency_p95_ms"`
	LatencyP99Ms  *float64 `json:"latency_p99_ms"`
}

// DependentsQuery scopes QueryDirectDependents. Env is an exact environment;
// empty means every environment.
type DependentsQuery struct {
	Service string
	Env     string
	Window  TimeWindow
}

// Dependent is a service that calls the queried service directly.
type Dependent struct {
	Service       string  `json:"service"`
	ThroughputRPM float64 `json:"throughput_rpm"`
}

// QueryGoldenSignals returns golden signals keyed by service name. Counts are
// summed across environments; latencies are the slowest environment's. Services
// with no series are absent from the map.
func QueryGoldenSignals(ctx context.Context, client *http.Client, cfg models.Config, q GoldenSignalsQuery) (map[string]GoldenSignals, error) {
	matcher := goldenSignalsMatcher(q.Services, q.Env)
	windowMin := intervalMinutes(q.Window.Start.Unix(), q.Window.End.Unix())
	end := q.Window.End.Unix()

	rows, err := summarizeServiceCounts(ctx, client, cfg, serviceCountScope{matcher: matcher, windowMin: windowMin, end: end})
	if err != nil {
		return nil, err
	}
	signals := make(map[string]GoldenSignals)
	for _, row := range rows {
		s := signals[row.Service]
		s.RequestCount += row.RequestCount
		s.ErrorCount += row.HTTP5xxCount + row.GRPCErrorCount
		signals[row.Service] = s
	}
	for service, s := range signals {
		s.ThroughputRPM = s.RequestCount / float64(windowMin)
		if s.RequestCount > 0 {
			rate := s.ErrorCount / s.RequestCount * 100
			s.ErrorRatePct = &rate
		}
		signals[service] = s
	}

	latency, err := fetchPromInstantSeries(ctx, client, cfg, "golden signals latency", goldenLatencyQuery(matcher, windowMin), end)
	if err != nil {
		return nil, err
	}
	for _, r := range latency {
		service := r.Metric["service_name"]
		if service == "" {
			continue
		}
		val, err := parsePromInstantValue(r.Value)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}
		s := signals[service]
		switch r.Metric["quantile"] {
		case "p50":
			s.LatencyP50Ms = &val
		case "p95":
			s.LatencyP95Ms = &val
		case "p99":
			s.LatencyP99Ms = &val
		}
		signals[service] = s
	}
	return signals, nil
}

// QueryDirectDependents returns the services calling q.Service, busiest first.
// Self-calls and callers without a service name are excluded.
func QueryDirectDependents(ctx context.Context, client *http.Client, cfg models.Config, q DependentsQuery) ([]Dependent, error) {
	windowMin := intervalMinutes(q.Window.Start.Unix(), q.Window.End.Unix())
	query := incomingCallersQuery(utils.EscapePromQLLabel(q.Service), utils.EscapePromQLLabel(exactEnvPattern(q.Env)), windowMin)
	series, err := fetchPromInstantSeries(ctx, client, cfg, "direct dependents", query, q.Window.End.Unix())
	if err != nil {
		return nil, err
	}
	dependents := make([]Dependent, 0, len(series))
	for _, r := range series {
		caller := r.Metric["client"]
		if caller == "" || caller == q.Service {
			continue
		}
		val, err := parsePromInstantValue(r.Value)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}
		dependents = append(dependents, Dependent{Service: caller, ThroughputRPM: val})
	}
	sort.SliceStable(dependents, func(i, j int) bool {
		if dependents[i].ThroughputRPM != dependents[j].ThroughputRPM {
			return dependents[i].ThroughputRPM > dependents[j].ThroughputRPM
		}
		return dependents[i].Service < dependents[j].Service
	})
	return dependents, nil
}

// goldenSignalsMatcher selects the given services in one exact environment.
// Names are regex-quoted, then the whole pattern is escaped for the PromQL
// string literal.
func goldenSignalsMatcher(services []string, env string) string {
	quoted := make([]string, len(services))
	for i, s := range services {
		quoted[i] = regexp.QuoteMeta(s)
	}
	return fmt.Sprintf(`env=~"%s",service_name=~"%s"`,
		utils.EscapePromQLLabel(exactEnvPattern(env)), utils.EscapePromQLLabel(strings.Join(quoted, "|")))
}

// goldenLatencyQuery aggregates each latency quantile series within one env the
// way get_service_dependency_graph and get_service_operations_summary do, then
// keeps the slowest env: quantile gauges from different envs cannot be summed.
// The window is a subquery over the summed gauge: a range inside sum would be
// reduced to one sample before quantile_over_time sees it.
func goldenLatencyQuery(matcher string, windowMin int) string {
	return fmt.Sprintf(
		`max by (service_name, quantile) (quantile_over_time(0.95, (sum by (service_name, env, quantile) (trace_service_response_time{%s, quantile=~"p50|p95|p99"}))[%dm:1m]))`,
		matcher, windowMin,
	)
}

func exactEnvPattern(env string) string {
	if env == "" {
		return ".*"
	}
	return regexp.QuoteMeta(env)
}
