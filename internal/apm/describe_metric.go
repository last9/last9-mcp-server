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
	"last9-mcp/internal/metriccatalog"
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
	TypeSource       string   `json:"type_source"`
	Confidence       string   `json:"confidence"`
	MetadataStatus   string   `json:"metadata_status,omitempty"`
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
		reader := metricDescriber{api: last9api.NewClient(client, choice.cfg), datasource: promDatasource(choice.cfg), timestamp: utils.Now().Unix()}
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
	result := MetricDescription{Name: name, Type: "unknown", TypeSource: "unknown", Confidence: "unknown", LabelKeys: []string{}, Notes: []string{}}
	labels, notes, err := r.labels(ctx, name)
	if err != nil {
		return result, err
	}
	result.LabelKeys, result.Notes = labels, notes
	family, err := r.resolveType(ctx, &result)
	if err != nil {
		return result, err
	}
	if result.Type == "unknown" {
		result.Notes = append(result.Notes, "Do not use rate()/increase() until verified; metric semantics are unknown.")
	}
	switch result.Type {
	case "counter":
		if !result.Inferred {
			result.RecommendedQuery = "rate(" + name + "[5m])"
		} else {
			result.RecommendedQuery = name
			result.Notes = append(result.Notes, "Counter-like evidence is inferred, not proof of a cumulative counter; do not use rate()/increase() until verified.")
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
		if result.TypeSource != "probe" {
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
		result.Notes = append(result.Notes, "Metadata has an unrecognized type; no query recommendation is applied.")
	}
	if result.Unit == "" {
		result.Unit = boundedMetricText(*first.Unit, 128, "unit", result)
	}
	result.Help = boundedMetricText(*first.Help, 1024, "help", result)
}

func boundedMetricText(value string, limit int, field string, result *MetricDescription) string {
	if len(value) <= limit {
		return value
	}
	result.Notes = append(result.Notes, field+" truncated to its byte limit.")
	return strings.ToValidUTF8(value[:limit], "")
}

func traceQuantileMetric(name string) bool {
	d, ok := metriccatalog.Last9(name)
	return ok && d.Quantile
}

// labelSeriesLimit bounds the series read per label lookup; keys are their union.
const labelSeriesLimit = 50

func (r metricDescriber) labels(ctx context.Context, name string) ([]string, []string, error) {
	notes := []string{fmt.Sprintf("Label keys are observed on at most %d series, not exhaustive; lookback is 60 minutes, widened to 4 days only when no series is found.", labelSeriesLimit)}
	for _, window := range []int64{int64(utils.DefaultLookbackMinutes) * 60, sparseLookbackMinutes * 60} {
		// The label endpoint can answer with keys from other metrics; only the metric's own series are evidence.
		query := fmt.Sprintf("topk(%d, last_over_time(%s[%ds]))", labelSeriesLimit, name, window)
		raw, err := prom.QueryInstantRaw(ctx, r.api, prom.QueryInstantInput{Datasource: r.datasource, Query: query, Timestamp: r.timestamp}, last9api.WithMaxResponseBytes(256<<10))
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if err != nil {
			if metricDiscoveryMustFail(err) {
				return nil, nil, err
			}
			return []string{}, append(notes, "Incomplete label discovery: label discovery failed; retry with prometheus_labels."), nil
		}
		labels, seriesCount, err := decodeSeriesLabels(raw)
		if err != nil {
			return []string{}, append(notes, "Incomplete label discovery: label discovery failed because the response was invalid or missing."), nil
		}
		if seriesCount == 0 {
			continue
		}
		slices.Sort(labels)
		labels = slices.Compact(labels)
		keys := make([]string, 0, 20)
		// Type inference and query guidance require these keys even when display labels are capped.
		for _, key := range []string{"le", "quantile"} {
			if slices.Contains(labels, key) {
				keys = append(keys, key)
			}
		}
		truncated := false
		for _, key := range labels {
			if key == "__name__" || key == "le" || key == "quantile" {
				continue
			}
			if len(key) > 128 || len(keys) == 20 {
				truncated = true
				continue
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if truncated {
			notes = append(notes, "Label keys truncated: at most 20 keys, with keys over 128 bytes omitted.")
		}
		if window == sparseLookbackMinutes*60 {
			notes = append(notes, "Labels were observed over the wider 4-day lookback.")
		}
		return keys, notes, nil
	}
	return []string{}, append(notes, "No series observed in either lookback; metric existence remains unverified."), nil
}

func decodeSeriesLabels(raw []byte) (labels []string, seriesCount int, err error) {
	var series *[]prom.InstantSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		return nil, 0, err
	}
	if series == nil {
		return nil, 0, errors.New("missing series")
	}
	for _, s := range *series {
		for key := range s.Metric {
			if strings.TrimSpace(key) == "" {
				return nil, 0, errors.New("empty label key")
			}
			labels = append(labels, key)
		}
	}
	return labels, len(*series), nil
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

func (r metricDescriber) resolveType(ctx context.Context, result *MetricDescription) (string, error) {
	name := result.Name
	if d, ok := metriccatalog.Last9(name); ok {
		result.Type, result.Unit, result.Help = d.Type, d.Unit, d.Help
		result.TypeSource, result.Confidence = "catalog", "high"
		return name, nil
	}
	family := metricFamily(name)
	if family != name {
		if _, ok := metriccatalog.Last9(family); ok {
			result.Notes = append(result.Notes, "The catalog family is a gauge, not a histogram; suffixed components cannot establish histogram semantics.")
			return name, nil
		}
	}
	if d, ok := metriccatalog.Receiver(name); ok {
		applyReceiverDefinition(result, d)
		return name, nil
	}
	if family != name {
		if d, ok := metriccatalog.Receiver(family); ok && d.Type == "histogram" {
			applyReceiverDefinition(result, d)
			return family, nil
		}
	}
	result.Unit = metricSuffixUnit(name)
	switch {
	case slices.Contains(result.LabelKeys, "quantile"):
		result.Type = "summary"
	case strings.HasSuffix(name, "_total"), strings.HasSuffix(name, "_created"):
		result.Type = "counter"
	case strings.HasSuffix(name, "_bucket") && slices.Contains(result.LabelKeys, "le"):
		result.Type = "histogram"
	case strings.HasSuffix(name, "_sum"), strings.HasSuffix(name, "_count"):
		labels, notes, err := r.labels(ctx, family+"_bucket")
		if err != nil {
			return name, err
		}
		for _, note := range notes {
			result.Notes = append(result.Notes, "Family discovery: "+note)
		}
		if slices.Contains(labels, "le") {
			result.Type = "histogram"
		} else {
			result.Notes = append(result.Notes, "The _sum/_count suffix is ambiguous without histogram or summary family evidence.")
		}
	}
	if result.Type != "unknown" {
		result.TypeSource, result.Confidence, result.Inferred = "naming", "medium", true
		result.Notes = append(result.Notes, "Type is inferred from naming and observed labels.")
		if result.Type == "histogram" {
			return family, nil
		}
		return name, nil
	}
	if family == name {
		kind, note, err := r.probeType(ctx, name)
		if err != nil {
			return name, err
		}
		if kind != "unknown" {
			result.Type, result.TypeSource, result.Confidence, result.Inferred = kind, "probe", "low", true
			result.Notes = append(result.Notes, "Type is inferred from one series over 5 minutes at a 30-second step; a sample's shape does not establish instrument semantics.")
			return name, nil
		}
		result.Notes = append(result.Notes, note)
	}
	metadataCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	definitions, metadataFamily, err := r.metadata(metadataCtx, name)
	if ctx.Err() != nil {
		return name, ctx.Err()
	}
	if err != nil {
		if metricDiscoveryMustFail(err) && !errors.Is(err, context.DeadlineExceeded) {
			return name, err
		}
		result.MetadataStatus = "metadata_error"
		var upstream *utils.UpstreamHTTPError
		if errors.Is(err, prom.ErrMetadataUnsupported) || (errors.As(err, &upstream) && upstream.StatusCode == 404) {
			result.MetadataStatus = "metadata_unavailable"
		}
		result.Notes = append(result.Notes, result.MetadataStatus+": optional metadata could not be read; it is not required for this tool.")
		return name, nil
	}
	if len(definitions) == 0 {
		result.MetadataStatus = "metadata_unavailable"
		result.Notes = append(result.Notes, "metadata_unavailable: empty metadata does not establish that the metric is absent.")
		return name, nil
	}
	result.MetadataStatus = "available"
	applyMetricDefinitions(result, definitions)
	if result.Type != "unknown" {
		result.TypeSource, result.Confidence = "metadata", "high"
	}
	return metadataFamily, nil
}

func applyReceiverDefinition(result *MetricDescription, d metriccatalog.Definition) {
	result.Type, result.Unit, result.Help = d.Type, d.Unit, boundedMetricText(d.Help, 1024, "help", result)
	result.TypeSource, result.Confidence = "receiver", "high"
	result.Notes = append(result.Notes, "Receiver definition: "+d.Source+"; exporter renames or different versions can change semantics.")
}

func metricFamily(name string) string {
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if family, ok := strings.CutSuffix(name, suffix); ok && prom.ValidMetricName(family) {
			return family
		}
	}
	return name
}

func metricSuffixUnit(name string) string {
	name = metricFamily(strings.TrimSuffix(strings.TrimSuffix(name, "_total"), "_created"))
	for _, unit := range []string{"seconds", "milliseconds", "bytes", "ratio", "percent"} {
		if strings.HasSuffix(name, "_"+unit) {
			return unit
		}
	}
	return ""
}

// probeType returns the sampled shape, or "unknown" with a note naming why.
func (r metricDescriber) probeType(ctx context.Context, name string) (string, string, error) {
	const inconclusive = "Probe inconclusive or unavailable; no type inferred from missing samples."
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Freeze the selected label set, not its values: topk at each step could mix different series.
	query := fmt.Sprintf("(%s and topk(1, %s @ %d))[5m:30s]", name, name, r.timestamp)
	raw, err := prom.QueryInstantRaw(probeCtx, r.api, prom.QueryInstantInput{Datasource: r.datasource, Query: query, Timestamp: r.timestamp}, last9api.WithMaxResponseBytes(32<<10))
	if ctx.Err() != nil {
		return "unknown", "", ctx.Err()
	}
	var tooLarge *last9api.ResponseTooLargeError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "unknown", "Probe stopped at its 2-second limit; no type inferred.", nil
	case errors.As(err, &tooLarge):
		return "unknown", "Probe stopped at its 32 KiB response limit; no type inferred.", nil
	case err != nil && metricDiscoveryMustFail(err):
		return "unknown", "", err
	case err != nil:
		return "unknown", inconclusive, nil
	}
	if kind := classifyMetricSamples(raw, r.timestamp); kind != "unknown" {
		return kind, "", nil
	}
	return "unknown", inconclusive, nil
}

func classifyMetricSamples(raw []byte, timestamp int64) string {
	var series []struct {
		Values [][]any `json:"values"`
	}
	if json.Unmarshal(raw, &series) != nil || len(series) != 1 {
		return "unknown"
	}
	samples := series[0].Values
	if len(samples) < 4 || len(samples) > 11 {
		return "unknown"
	}
	rises, resets := 0, 0
	gaugeLike := false
	var previous, previousTime float64
	for i, sample := range samples {
		if len(sample) != 2 {
			return "unknown"
		}
		ts, ok := sample[0].(float64)
		if !ok || ts < float64(timestamp-300) || ts > float64(timestamp) || (i > 0 && ts-previousTime < 30) {
			return "unknown"
		}
		if _, ok := sample[1].(string); !ok {
			return "unknown"
		}
		value, err := parsePromInstantValue(sample)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return "unknown"
		}
		if value < 0 {
			gaugeLike = true
		}
		if i > 0 {
			switch {
			case value > previous:
				rises++
			case value < previous:
				if value >= 0 && value <= previous*0.1 && previous > 0 {
					resets++
				} else {
					gaugeLike = true
				}
			}
		}
		previous, previousTime = value, ts
	}
	if !gaugeLike && rises >= 2 && resets <= 1 {
		return "counter"
	}
	return "gauge"
}
