package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MetricStatusArgs struct {
	Metric        string  `json:"metric" jsonschema:"(Required) Metric name (e.g. up) or PromQL series selector (e.g. up{job=\"api\"})"`
	WindowMinutes float64 `json:"window_minutes" jsonschema:"(Required) Lookback window in minutes for liveness and cadence probes (minimum: 1). Prefer a window that can cover at least a few emission intervals for slow metrics (e.g. 5760 for daily CloudWatch metrics)."`
	Datasource    string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type MetricStatusResult struct {
	Live                    bool    `json:"live"`
	SeriesCount             int     `json:"series_count"`
	LastSampleAgeSeconds    float64 `json:"last_sample_age_seconds"`
	InferredIntervalSeconds float64 `json:"inferred_interval_seconds"`
	SuggestedWindow         string  `json:"suggested_window"`
}

func NewMetricStatusHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, MetricStatusArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args MetricStatusArgs) (*mcp.CallToolResult, any, error) {
		status, err := computeMetricStatus(ctx, client, cfg, args)
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(status)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal metric_status response: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	}
}

func computeMetricStatus(ctx context.Context, client *http.Client, cfg models.Config, args MetricStatusArgs) (MetricStatusResult, error) {
	metric := strings.TrimSpace(args.Metric)
	if metric == "" {
		return MetricStatusResult{}, fmt.Errorf("metric is required")
	}
	if args.WindowMinutes < 1 {
		return MetricStatusResult{}, fmt.Errorf("window_minutes must be at least 1")
	}

	queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
	if err != nil {
		return MetricStatusResult{}, err
	}

	endTime := time.Now().UTC().Unix()
	windowSec := int64(args.WindowMinutes * 60)
	startTime := endTime - windowSec
	dur := promDurationFromMinutes(args.WindowMinutes)
	sel := metricSelector(metric)

	countQuery := fmt.Sprintf("count(last_over_time(%s[%s]))", sel, dur)
	seriesCount, err := queryInstantScalar(ctx, client, queryCfg, countQuery, endTime)
	if err != nil {
		return MetricStatusResult{}, err
	}
	if seriesCount <= 0 {
		return MetricStatusResult{
			Live:        false,
			SeriesCount: 0,
		}, nil
	}

	ageQuery := fmt.Sprintf("time() - timestamp(%s[%s])", sel, dur)
	ages, err := queryInstantValues(ctx, client, queryCfg, ageQuery, endTime)
	if err != nil {
		return MetricStatusResult{}, err
	}
	lastAge := minPositiveOrZero(ages)

	interval, err := inferIntervalSeconds(ctx, client, queryCfg, sel, startTime, endTime)
	if err != nil {
		return MetricStatusResult{}, err
	}

	return MetricStatusResult{
		Live:                    true,
		SeriesCount:             int(math.Round(seriesCount)),
		LastSampleAgeSeconds:    lastAge,
		InferredIntervalSeconds: interval,
		SuggestedWindow:         suggestWindow(lastAge, interval),
	}, nil
}

func metricSelector(metric string) string {
	if strings.ContainsAny(metric, "{}()") {
		return metric
	}
	return fmt.Sprintf(`{__name__="%s"}`, utils.EscapePromQLLabel(metric))
}

func promDurationFromMinutes(minutes float64) string {
	if minutes <= 0 {
		return "1m"
	}
	if minutes == math.Trunc(minutes) {
		m := int64(minutes)
		if m%(24*60) == 0 {
			return fmt.Sprintf("%dd", m/(24*60))
		}
		if m%60 == 0 {
			return fmt.Sprintf("%dh", m/60)
		}
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%sm", strconv.FormatFloat(minutes, 'f', -1, 64))
}

func queryInstantScalar(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64) (float64, error) {
	vals, err := queryInstantValues(ctx, client, cfg, query, endTime)
	if err != nil {
		return 0, err
	}
	if len(vals) == 0 {
		return 0, nil
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum, nil
}

func queryInstantValues(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64) ([]float64, error) {
	httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime, cfg)
	if err != nil {
		return nil, err
	}
	if httpResp == nil {
		return nil, fmt.Errorf("received nil response from Prometheus")
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, promErr(httpResp, "metric_status instant query")
	}
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Prometheus response: %w", err)
	}
	var series apiPromInstantResp
	if err := json.Unmarshal(body, &series); err != nil {
		return nil, fmt.Errorf("failed to decode Prometheus instant response: %w", err)
	}
	out := make([]float64, 0, len(series))
	for _, s := range series {
		v, err := parsePromInstantValue(s.Value)
		if err != nil {
			continue
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func inferIntervalSeconds(ctx context.Context, client *http.Client, cfg models.Config, sel string, startTime, endTime int64) (float64, error) {
	httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, sel, startTime, endTime, cfg, utils.PromResolution{MaxDataPoints: 500})
	if err != nil {
		return 0, err
	}
	if httpResp == nil {
		return 0, fmt.Errorf("received nil response from Prometheus")
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return 0, promErr(httpResp, "metric_status range query")
	}
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read Prometheus range response: %w", err)
	}
	series, err := parsePromTimeSeries(body)
	if err != nil {
		return 0, err
	}
	return medianSampleDeltaSeconds(series), nil
}

func medianSampleDeltaSeconds(series []TimeSeries) float64 {
	deltas := make([]float64, 0)
	for _, s := range series {
		if len(s.Values) < 2 {
			continue
		}
		for i := 1; i < len(s.Values); i++ {
			d := float64(s.Values[i].Timestamp) - float64(s.Values[i-1].Timestamp)
			if d > 0 {
				deltas = append(deltas, d)
			}
		}
	}
	if len(deltas) == 0 {
		return 0
	}
	sort.Float64s(deltas)
	mid := len(deltas) / 2
	if len(deltas)%2 == 0 {
		return (deltas[mid-1] + deltas[mid]) / 2
	}
	return deltas[mid]
}

func minPositiveOrZero(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	min := vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
	}
	if min < 0 {
		return 0
	}
	return min
}

// High-cadence metrics keep Grafana's $__interval; slow emitters need a window
// that exceeds cadence (~3× max(age, interval)) so last_over_time does not go blank
// between samples — including right after a fresh emission when age ≪ cadence.
func suggestWindow(lastSampleAgeSeconds, inferredIntervalSeconds float64) string {
	const liveCadenceSeconds = 5 * 60
	if inferredIntervalSeconds > 0 && inferredIntervalSeconds <= liveCadenceSeconds {
		return "$__interval"
	}
	if inferredIntervalSeconds <= 0 && lastSampleAgeSeconds > 0 && lastSampleAgeSeconds <= liveCadenceSeconds {
		return "$__interval"
	}
	basis := math.Max(lastSampleAgeSeconds, inferredIntervalSeconds)
	if basis <= 0 {
		return "$__interval"
	}
	return formatSuggestedDuration(math.Ceil(basis * 3))
}

func formatSuggestedDuration(seconds float64) string {
	sec := int64(math.Round(seconds))
	if sec < 1 {
		sec = 1
	}
	day := int64(24 * 60 * 60)
	hour := int64(60 * 60)
	minute := int64(60)
	if sec%day == 0 {
		return fmt.Sprintf("%dd", sec/day)
	}
	if sec >= day {
		days := int64(math.Ceil(float64(sec) / float64(day)))
		return fmt.Sprintf("%dd", days)
	}
	if sec%hour == 0 {
		return fmt.Sprintf("%dh", sec/hour)
	}
	if sec >= hour {
		hours := int64(math.Ceil(float64(sec) / float64(hour)))
		return fmt.Sprintf("%dh", hours)
	}
	if sec%minute == 0 {
		return fmt.Sprintf("%dm", sec/minute)
	}
	mins := int64(math.Ceil(float64(sec) / float64(minute)))
	return fmt.Sprintf("%dm", mins)
}
