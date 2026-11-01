package apm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DescribeMetricArgs struct {
	MetricNames []string `json:"metric_names" jsonschema:"(Required) 1–10 exact metric names, each at most 256 bytes; no selectors or expressions."`
	Datasource  string   `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match, ambiguous names return candidates."`
}

type MetricDescription struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	Inferred         bool     `json:"inferred"`
	Unit             string   `json:"unit,omitempty"`
	Help             string   `json:"help,omitempty"`
	LabelKeys        []string `json:"label_keys"`
	RecommendedQuery string   `json:"recommended_query"`
	Notes            []string `json:"notes"`
}

type DescribeMetricResult struct {
	Metrics []MetricDescription `json:"metrics"`
}

type metricDescriber struct {
	api        *last9api.Client
	datasource prom.Datasource
	timestamp  int64
}

func NewDescribeMetricHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DescribeMetricArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DescribeMetricArgs) (*mcp.CallToolResult, any, error) {
		if len(args.MetricNames) < 1 || len(args.MetricNames) > 10 {
			return nil, nil, errors.New("metric_names requires 1–10 names")
		}
		for _, name := range args.MetricNames {
			if len(name) > 256 || !prom.ValidMetricName(name) {
				return nil, nil, fmt.Errorf("invalid metric name %q: use a name of at most 256 bytes, not a selector or expression", shorten(name))
			}
		}
		choice, err := chooseDatasource(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		if choice.candidates != nil {
			return ambiguousDatasourceResult(args.Datasource, choice), nil, nil
		}
		reader := metricDescriber{api: last9api.NewClient(client, choice.cfg), datasource: promDatasource(choice.cfg), timestamp: time.Now().Unix()}
		report := DescribeMetricResult{Metrics: make([]MetricDescription, 0, len(args.MetricNames))}
		for _, name := range args.MetricNames {
			metric, err := reader.describe(ctx, name)
			if err != nil {
				return nil, nil, err
			}
			report.Metrics = append(report.Metrics, metric)
		}
		body, err := json.Marshal(report)
		if err != nil {
			return nil, nil, err
		}
		if len(body)+len(choice.note) > 256<<10 {
			return nil, nil, errors.New("metric description exceeds 256 KiB; request fewer metrics")
		}
		return withDatasourceNote(textResult(body), choice.note), nil, nil
	}
}

func (r metricDescriber) describe(ctx context.Context, name string) (MetricDescription, error) {
	result := MetricDescription{Name: name, Type: "unknown", LabelKeys: []string{}, Notes: []string{}}
	definitions, family, err := r.metadata(ctx, name)
	if err != nil && !errors.Is(err, prom.ErrMetadataUnsupported) {
		return result, err
	}
	if errors.Is(err, prom.ErrMetadataUnsupported) {
		result.Notes = append(result.Notes, "Metadata is unsupported by this datasource.")
	}
	if len(definitions) == 0 {
		result.Notes = append(result.Notes, "Metadata is unavailable; this does not establish that the metric is absent.")
		inferMetricType(&result)
	} else {
		applyMetricDefinitions(&result, definitions)
	}
	labels, notes, err := r.labels(ctx, name)
	if err != nil {
		return result, err
	}
	if traceQuantileMetric(name) && (result.Type == "counter" || result.Type == "histogram") {
		result.Type = "unknown"
		result.Notes = append(result.Notes, "Metadata conflicts with the documented pre-windowed trace latency family; no query is recommended.")
	}
	result.LabelKeys = labels
	result.Notes = append(result.Notes, notes...)
	switch result.Type {
	case "counter":
		if !result.Inferred {
			result.RecommendedQuery = "rate(" + name + "[5m])"
		}
	case "gauge":
		result.RecommendedQuery = name
		result.Notes = append(result.Notes, "Never apply rate()/increase() to gauges, including pre-windowed gauges.")
		if traceQuantileMetric(name) {
			result.RecommendedQuery = name + `{quantile="p95"}`
			result.Notes = append(result.Notes, "This pre-windowed latency gauge uses quantile=\"p95\"; do not use histogram_quantile or average quantiles into a wider-window percentile.")
		}
	case "summary":
		if traceQuantileMetric(name) {
			result.RecommendedQuery = name + `{quantile="p95"}`
		} else if family == name {
			result.RecommendedQuery = name + `{quantile!=""}`
		} else {
			result.Notes = append(result.Notes, "This is a summary component; select quantiles on the family metric instead.")
		}
		result.Notes = append(result.Notes, "Select existing summary quantiles directly. Never average quantiles into a wider-window percentile.")
	case "histogram":
		if !result.Inferred {
			bucket := family + "_bucket"
			if strings.HasSuffix(name, "_bucket") && slices.Contains(labels, "le") {
				bucket = name
			}
			bucketLabels := labels
			if bucket != name {
				var bucketNotes []string
				bucketLabels, bucketNotes, err = r.labels(ctx, bucket)
				if err != nil {
					return result, err
				}
				for _, note := range bucketNotes {
					result.Notes = append(result.Notes, "Bucket discovery: "+note)
				}
			}
			if slices.Contains(bucketLabels, "le") {
				result.RecommendedQuery = "histogram_quantile(0.95, sum by (le) (rate(" + bucket + "[5m])))"
				result.Notes = append(result.Notes, "The query combines observed classic histogram buckets; add grouping labels alongside le to keep populations separate.")
			} else {
				native, note, err := r.nativeHistogram(ctx, family)
				if err != nil {
					return result, err
				}
				result.Notes = append(result.Notes, note)
				if native {
					result.RecommendedQuery = "histogram_quantile(0.95, rate(" + family + "[5m]))"
				}
			}
		}
	}
	return result, nil
}

func (r metricDescriber) metadata(ctx context.Context, name string) ([]prom.MetadataDefinition, string, error) {
	get := func(metric string) ([]prom.MetadataDefinition, error) {
		response, err := prom.GetMetadata(ctx, r.api, prom.GetMetadataInput{Datasource: r.datasource, Metric: metric})
		return response.Data[metric], err
	}
	definitions, err := get(name)
	if err != nil || len(definitions) > 0 {
		return definitions, name, err
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		family, ok := strings.CutSuffix(name, suffix)
		if !ok || !prom.ValidMetricName(family) {
			continue
		}
		definitions, err = get(family)
		if err != nil {
			return nil, name, err
		}
		if len(definitions) > 0 {
			for _, d := range definitions {
				if d.Type == "histogram" || d.Type == "summary" {
					return definitions, family, nil
				}
			}
		}
	}
	return nil, name, nil
}

func applyMetricDefinitions(result *MetricDescription, definitions []prom.MetadataDefinition) {
	first := definitions[0]
	for _, d := range definitions[1:] {
		if d.Type != first.Type || *d.Unit != *first.Unit || *d.Help != *first.Help {
			result.Notes = append(result.Notes, "There are conflicting metadata definitions (type, unit or help); no definition was selected and no query is recommended.")
			return
		}
	}
	switch first.Type {
	case "counter", "gauge", "histogram", "summary":
		result.Type = first.Type
	default:
		result.Notes = append(result.Notes, "Metadata has an unrecognized type; no naming fallback or query recommendation is applied.")
	}
	result.Unit = boundedMetricText(*first.Unit, 128, "unit", result)
	result.Help = boundedMetricText(*first.Help, 1024, "help", result)
}

func boundedMetricText(value string, limit int, field string, result *MetricDescription) string {
	if len(value) <= limit {
		return value
	}
	result.Notes = append(result.Notes, field+" truncated to its byte limit.")
	return strings.ToValidUTF8(value[:limit], "")
}

func inferMetricType(result *MetricDescription) {
	switch {
	case traceQuantileMetric(result.Name):
		result.Type = "gauge"
	case strings.HasSuffix(result.Name, "_total"):
		result.Type = "counter"
	case strings.HasSuffix(result.Name, "_bucket"):
		result.Type = "histogram"
	case strings.HasSuffix(result.Name, "_sum"), strings.HasSuffix(result.Name, "_count"):
		result.Notes = append(result.Notes, "The _sum/_count suffix is ambiguous without histogram or summary family metadata.")
	}
	if result.Type != "unknown" {
		result.Inferred = true
		result.Notes = append(result.Notes, "Type is inferred from naming, not verified metadata. Verify the type before rate()/increase() or histogram queries.")
	}
}

func traceQuantileMetric(name string) bool {
	switch name {
	case "trace_service_response_time", "trace_endpoint_duration", "trace_client_duration", "trace_call_graph_duration", "trace_internal_call_graph_duration":
		return true
	default:
		return false
	}
}

func (r metricDescriber) labels(ctx context.Context, name string) ([]string, []string, error) {
	notes := []string{"Label keys are observed, not exhaustive; lookback is 60 minutes, widened to 4 days only when empty."}
	for _, window := range []int64{int64(utils.DefaultLookbackMinutes) * 60, sparseLookbackMinutes * 60} {
		raw, err := prom.GetAPMLabels(ctx, r.api, prom.GetAPMLabelsInput{Datasource: r.datasource, Metric: name, Timestamp: r.timestamp, Window: window}, last9api.WithMaxResponseBytes(256<<10))
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if err != nil {
			if metricDiscoveryMustFail(err) {
				return nil, nil, err
			}
			return []string{}, append(notes, "Incomplete label discovery: label discovery failed; retry with prometheus_labels."), nil
		}
		labels, err := decodeMetricLabels(raw)
		if err != nil {
			return []string{}, append(notes, "Incomplete label discovery: label discovery failed because the response was invalid or missing."), nil
		}
		if len(labels) == 0 {
			continue
		}
		slices.Sort(labels)
		labels = slices.Compact(labels)
		keys := make([]string, 0, 20)
		truncated := false
		for _, key := range labels {
			if key == "__name__" {
				continue
			}
			if len(key) > 128 || len(keys) == 20 {
				truncated = true
				continue
			}
			keys = append(keys, key)
		}
		if truncated {
			notes = append(notes, "Label keys truncated: at most 20 keys, with keys over 128 bytes omitted.")
		}
		if window == sparseLookbackMinutes*60 {
			notes = append(notes, "Labels were observed over the wider 4-day lookback.")
		}
		return keys, notes, nil
	}
	return []string{}, append(notes, "No label keys observed in either lookback; metric existence remains unverified."), nil
}

func decodeMetricLabels(raw []byte) ([]string, error) {
	var labels []string
	if json.Unmarshal(raw, &labels) == nil && labels != nil {
		return validateMetricLabels(labels)
	}
	var envelope struct {
		Labels *[]string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Labels == nil {
		return nil, errors.New("missing labels")
	}
	return validateMetricLabels(*envelope.Labels)
}

func validateMetricLabels(labels []string) ([]string, error) {
	for _, label := range labels {
		if strings.TrimSpace(label) == "" {
			return nil, errors.New("empty label key")
		}
	}
	return labels, nil
}

func (r metricDescriber) nativeHistogram(ctx context.Context, name string) (bool, string, error) {
	raw, err := prom.QueryInstantRaw(ctx, r.api, prom.QueryInstantInput{Datasource: r.datasource, Query: "count(histogram_count(" + name + "))", Timestamp: r.timestamp}, last9api.WithMaxResponseBytes(256<<10))
	if ctx.Err() != nil {
		return false, "", ctx.Err()
	}
	if err != nil {
		if metricDiscoveryMustFail(err) {
			return false, "", err
		}
		return false, "Incomplete discovery: native histogram discovery failed; no bucket series is assumed.", nil
	}
	var samples []prom.InstantSeries
	if err := json.Unmarshal(raw, &samples); err != nil || samples == nil {
		return false, "Incomplete discovery: native histogram discovery failed because its response was invalid or missing.", nil
	}
	if len(samples) > 0 {
		if len(samples) != 1 {
			return false, "Incomplete discovery: native histogram discovery failed because the sample count was invalid.", nil
		}
		count, err := parsePromInstantValue(samples[0].Value)
		if err != nil || math.IsInf(count, 0) || math.IsNaN(count) || count < 0 || count != math.Trunc(count) {
			return false, "Incomplete discovery: native histogram discovery failed because the sample count was invalid.", nil
		}
		if count > 0 {
			return true, "Native histogram samples were observed at the current evaluation time; use the native metric directly, without a _bucket suffix.", nil
		}
	}
	return false, "Classic buckets and native histogram samples were not observed; incomplete discovery is possible. No bucket series is assumed.", nil
}

func metricDiscoveryMustFail(err error) bool {
	var upstream *utils.UpstreamHTTPError
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &upstream) && (upstream.StatusCode == 401 || upstream.StatusCode == 403 || upstream.StatusCode == 499))
}
