package apm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type metricDescriptionFixture struct {
	probe        string
	probeDelay   time.Duration
	probeStatus  int
	nativeCount  string
	nativeStatus int
	metadata     map[string]string
	labels       map[string]string // metric -> JSON list of label keys, served as one series
	labelBodies  map[string]string // metric -> raw label-lookup response, overriding labels
	status       int
	labelStatus  int
	requests     []map[string]any
}

func (f *metricDescriptionFixture) handler(t *testing.T) func(context.Context, *mcp.CallToolRequest, DescribeMetricArgs) (*mcp.CallToolResult, any, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["path"] = r.URL.Path
		f.requests = append(f.requests, body)
		metric, _ := body["metric"].(string)
		switch r.URL.Path {
		case "/prom_metadata":
			if f.status != 0 {
				w.WriteHeader(f.status)
				return
			}
			if defs, ok := f.metadata[metric]; ok {
				fmt.Fprintf(w, `{"status":"success","data":{%q:%s}}`, metric, defs)
			} else {
				fmt.Fprint(w, `{"status":"success","data":{}}`)
			}
		case "/prom_query_instant":
			query := fmt.Sprint(body["query"])
			if _, rest, ok := strings.Cut(query, "last_over_time("); ok {
				if f.labelStatus != 0 {
					w.WriteHeader(f.labelStatus)
					return
				}
				metric, _, _ := strings.Cut(rest, "[")
				if raw, ok := f.labelBodies[metric]; ok {
					fmt.Fprint(w, raw)
					return
				}
				fmt.Fprint(w, seriesWithLabels(t, f.labels[metric]))
				return
			}
			if strings.Contains(query, "[5m:30s]") {
				select {
				case <-time.After(f.probeDelay):
				case <-r.Context().Done():
					return
				}
				if f.probeStatus != 0 {
					w.WriteHeader(f.probeStatus)
					return
				}
				if f.probe == "" {
					fmt.Fprint(w, `[]`)
				} else {
					fmt.Fprint(w, f.probe)
				}
				return
			}
			if f.nativeStatus != 0 {
				w.WriteHeader(f.nativeStatus)
				return
			}
			value := f.nativeCount
			if value == "" {
				value = "0"
			}
			fmt.Fprintf(w, `[{"metric":{},"value":[1700000000,%q]}]`, value)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{APIBaseURL: srv.URL, PrometheusReadURL: "https://prom.example", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
	return NewDescribeMetricHandler(srv.Client(), cfg)
}

// seriesWithLabels answers a label lookup with one series carrying every key; no keys means no series.
func seriesWithLabels(t *testing.T, keysJSON string) string {
	var keys []string
	if keysJSON != "" {
		if err := json.Unmarshal([]byte(keysJSON), &keys); err != nil {
			t.Errorf("label fixture %q: %v", keysJSON, err)
		}
	}
	if len(keys) == 0 {
		return `[]`
	}
	metric := map[string]string{}
	for _, key := range keys {
		metric[key] = "v"
	}
	raw, _ := json.Marshal([]prom.InstantSeries{{Metric: metric, Value: []any{1700000000, "1"}}})
	return string(raw)
}

func metricDefinition(kind string) string {
	return fmt.Sprintf(`[{"type":%q,"unit":"seconds","help":"Description"}]`, kind)
}

func describeOne(t *testing.T, f *metricDescriptionFixture, name string) MetricDescription {
	t.Helper()
	res, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	var report DescribeMetricResult
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Metrics) != 1 {
		t.Fatalf("report=%+v", report)
	}
	return report.Metrics[0]
}

func TestDescribeMetricRecommendations(t *testing.T) {
	for _, tt := range []struct {
		name, metric, kind, family, labels, wantType, query, note string
		inferred                                                  bool
	}{
		{name: "metadata counter", metric: "requests", kind: "counter", wantType: "counter", query: "rate(requests[5m])"},
		{name: "metadata gauge", metric: "requests", kind: "gauge", wantType: "gauge", query: "requests", note: "Never apply rate"},
		{name: "classic family", metric: "latency", kind: "histogram", labels: `["le","job"]`, wantType: "histogram", query: "histogram_quantile(0.95, sum by (le) (rate(latency_bucket[5m])))"},
		{name: "bucket component", metric: "latency_bucket", family: "latency", labels: `["le"]`, wantType: "histogram", inferred: true, query: "histogram_quantile(0.95, sum by (le) (rate(latency_bucket[5m])))"},
		{name: "count component", metric: "latency_count", family: "latency", labels: `["le"]`, wantType: "histogram", inferred: true, query: "histogram_quantile(0.95, sum by (le) (rate(latency_bucket[5m])))"},
		{name: "sum component", metric: "latency_sum", family: "latency", kind: "summary", wantType: "summary", note: "component"},
		{name: "histogram without buckets", metric: "latency", kind: "histogram", wantType: "histogram", note: "native"},
		{name: "summary", metric: "size", kind: "summary", wantType: "summary", query: `size{quantile!=""}`, note: "Never average"},
		{name: "inferred counter", metric: "requests_total", wantType: "counter", inferred: true, query: "requests_total", note: "inferred"},
		{name: "bucket without le", metric: "size_bucket", wantType: "unknown", note: "until verified"},
		{name: "ambiguous count", metric: "size_count", wantType: "unknown", note: "ambiguous"},
		{name: "ambiguous sum", metric: "size_sum", wantType: "unknown", note: "ambiguous"},
		{name: "unknown", metric: "size", wantType: "unknown", note: "unavailable"},
		{name: "trace latency", metric: "trace_service_response_time", wantType: "gauge", query: `trace_service_response_time{quantile="p95"}`, note: "pre-windowed"},
		{name: "trace requests", metric: "trace_requests", wantType: "unknown"},
		{name: "unrecognized metadata", metric: "size", kind: "untyped", wantType: "unknown", note: "unrecognized"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &metricDescriptionFixture{metadata: map[string]string{}, labels: map[string]string{}}
			metadataName := tt.metric
			if tt.family != "" {
				metadataName = tt.family
			}
			if tt.kind != "" {
				f.metadata[metadataName] = metricDefinition(tt.kind)
			}
			if tt.labels != "" {
				bucket := metadataName + "_bucket"
				f.labels[bucket] = tt.labels
			}
			got := describeOne(t, f, tt.metric)
			if got.Type != tt.wantType || got.Inferred != tt.inferred || got.RecommendedQuery != tt.query {
				t.Fatalf("got %+v", got)
			}
			if tt.note != "" && !strings.Contains(strings.Join(got.Notes, " "), tt.note) {
				t.Fatalf("missing note %q: %+v", tt.note, got)
			}
			if tt.kind != "" && (got.Unit != "seconds" || got.Help != "Description") {
				t.Fatalf("metadata lost: %+v", got)
			}
		})
	}
}

func TestDescribeMetricConflicts(t *testing.T) {
	for _, definitions := range []string{
		`[{"type":"counter","unit":"","help":"A"},{"type":"gauge","unit":"","help":"A"}]`,
		`[{"type":"counter","unit":"bytes","help":"A"},{"type":"counter","unit":"seconds","help":"A"}]`,
		`[{"type":"counter","unit":"","help":"A"},{"type":"counter","unit":"","help":"B"}]`,
	} {
		got := describeOne(t, &metricDescriptionFixture{metadata: map[string]string{"requests": definitions}}, "requests")
		if got.Type != "unknown" || got.Inferred || got.RecommendedQuery != "" || !strings.Contains(strings.Join(got.Notes, " "), "conflicting") {
			t.Fatalf("lost conflict: %+v", got)
		}
	}
}

func TestDescribeMetricValidatesBatchBeforeIO(t *testing.T) {
	for _, names := range [][]string{nil, {}, {"up", " "}, {"up", "up{}"}, {strings.Repeat("a", 257)}, strings.Fields(strings.Repeat("up ", 11))} {
		f := &metricDescriptionFixture{}
		_, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: names})
		if err == nil || len(f.requests) != 0 {
			t.Fatalf("names=%v err=%v requests=%v", names, err, f.requests)
		}
	}
}

func TestDescribeMetricFailures(t *testing.T) {
	for _, status := range []int{401, 403, 499} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := &metricDescriptionFixture{status: status}
			_, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{"unrecognized"}})
			var upstream *utils.UpstreamHTTPError
			if !errors.As(err, &upstream) || upstream.StatusCode != status {
				t.Fatalf("lost operational error: %v", err)
			}
		})
	}
	got := describeOne(t, &metricDescriptionFixture{status: 501}, "unrecognized")
	if got.TypeSource != "unknown" || got.MetadataStatus != "metadata_unavailable" {
		t.Fatalf("%+v", got)
	}
	for _, status := range []int{401, 403} {
		f := &metricDescriptionFixture{labelStatus: status}
		_, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{"up"}})
		if err == nil {
			t.Fatal("label auth swallowed")
		}
	}
	got = describeOne(t, &metricDescriptionFixture{labelStatus: 503}, "up")
	if !strings.Contains(strings.Join(got.Notes, " "), "label discovery failed") {
		t.Fatalf("%+v", got)
	}
}

func TestDescribeMetricLabelsAndBounds(t *testing.T) {
	labels := []string{"__name__"}
	for i := 0; i < 21; i++ {
		labels = append(labels, fmt.Sprintf("label_%02d", i))
	}
	raw, _ := json.Marshal(labels)
	f := &metricDescriptionFixture{labels: map[string]string{"up": string(raw)}, metadata: map[string]string{"up": fmt.Sprintf(`[{"type":"gauge","unit":"","help":%q}]`, strings.Repeat("x", 2000))}}
	got := describeOne(t, f, "up")
	if len(got.LabelKeys) != 20 || len(got.Help) > 1024 || !strings.Contains(strings.Join(got.Notes, " "), "truncated") {
		t.Fatalf("%+v", got)
	}
	f = &metricDescriptionFixture{}
	got = describeOne(t, f, "up")
	if got.LabelKeys == nil || !strings.Contains(strings.Join(got.Notes, " "), "not exhaustive") {
		t.Fatalf("%+v", got)
	}
	var queries []any
	for _, r := range f.requests {
		if strings.Contains(fmt.Sprint(r["query"]), "last_over_time(") {
			queries = append(queries, r["query"])
		}
	}
	if len(queries) != 2 || queries[0] != "topk(50, last_over_time(up[3600s]))" || queries[1] != "topk(50, last_over_time(up[345600s]))" {
		t.Fatalf("queries=%v", queries)
	}
	// A series without labels still proves the metric exists, so the wider lookback is not needed.
	f = &metricDescriptionFixture{labelBodies: map[string]string{"up": `[{"metric":{},"value":[1700000000,"1"]}]`}}
	got = describeOne(t, f, "up")
	if len(got.LabelKeys) != 0 || strings.Contains(strings.Join(got.Notes, " "), "existence remains unverified") || len(f.requests) == 0 || strings.Contains(fmt.Sprint(f.requests), "345600s") {
		t.Fatalf("%+v", got)
	}
}

func TestDescribeMetricTraceMetadata(t *testing.T) {
	for _, kind := range []string{"gauge", "summary", "histogram", "counter"} {
		got := describeOne(t, &metricDescriptionFixture{metadata: map[string]string{"trace_service_response_time": metricDefinition(kind)}, labels: map[string]string{"trace_service_response_time_bucket": `["le"]`}}, "trace_service_response_time")
		if strings.Contains(got.RecommendedQuery, "rate(") || strings.Contains(got.RecommendedQuery, "histogram_quantile") {
			t.Fatalf("unsafe trace query: %+v", got)
		}
		if (kind == "gauge" || kind == "summary") && got.RecommendedQuery != `trace_service_response_time{quantile="p95"}` {
			t.Fatalf("wrong quantile selection: %+v", got)
		}
	}
}

func TestDescribeMetricLabelFailuresAndCancellation(t *testing.T) {
	for _, raw := range []string{"", "null", `{}`, `["job"]`, `[{"metric":{"job":"a"}}] {}`, `[{"metric":{" ":"a"}}]`, strings.Repeat(" ", 256<<10) + `[]`} {
		got := describeOne(t, &metricDescriptionFixture{labelBodies: map[string]string{"up": raw}}, "up")
		if !strings.Contains(strings.Join(got.Notes, " "), "label discovery failed") {
			t.Fatalf("%q: %+v", raw[:min(len(raw), 50)], got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &metricDescriptionFixture{}
	_, _, err := f.handler(t)(ctx, nil, DescribeMetricArgs{MetricNames: []string{"up"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestDescribeMetricResponseBounds(t *testing.T) {
	for _, helpSize := range []int{1023, 1024, 1025} {
		f := &metricDescriptionFixture{metadata: map[string]string{"up": fmt.Sprintf(`[{"type":"gauge","unit":"","help":%q}]`, strings.Repeat("x", helpSize))}}
		got := describeOne(t, f, "up")
		if len(got.Help) != min(helpSize, 1024) || strings.Contains(strings.Join(got.Notes, " "), "help truncated") != (helpSize > 1024) {
			t.Fatalf("size=%d: %+v", helpSize, got)
		}
	}
	for _, n := range []int{19, 20, 21} {
		keys := make([]string, n)
		for i := range keys {
			keys[i] = fmt.Sprintf("label%02d", i)
		}
		raw, _ := json.Marshal(keys)
		got := describeOne(t, &metricDescriptionFixture{labels: map[string]string{"up": string(raw)}}, "up")
		if len(got.LabelKeys) != min(n, 20) || strings.Contains(strings.Join(got.Notes, " "), "Label keys truncated") != (n > 20) {
			t.Fatalf("n=%d: %+v", n, got)
		}
	}
	f := &metricDescriptionFixture{metadata: map[string]string{}, labels: map[string]string{}}
	names := make([]string, 10)
	for i := range names {
		names[i] = fmt.Sprintf("metric_%d", i)
		f.metadata[names[i]] = fmt.Sprintf(`[{"type":"gauge","unit":%q,"help":%q}]`, strings.Repeat("<", 200), strings.Repeat("<", 2000))
		keys := make([]string, 21)
		for j := range keys {
			keys[j] = fmt.Sprintf("%03d", j) + strings.Repeat("<", 125)
		}
		raw, _ := json.Marshal(keys)
		f.labels[names[i]] = string(raw)
	}
	res, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: names})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content[0].(*mcp.TextContent).Text) > 256<<10 {
		t.Fatal("unbounded response")
	}
}

func TestDescribeMetricNativeHistogram(t *testing.T) {
	for _, count := range []string{"0", "2"} {
		f := &metricDescriptionFixture{nativeCount: count, metadata: map[string]string{"latency": metricDefinition("histogram")}}
		got := describeOne(t, f, "latency")
		want := ""
		if count == "2" {
			want = "histogram_quantile(0.95, rate(latency[5m]))"
		}
		if got.RecommendedQuery != want {
			t.Fatalf("count=%s: %+v", count, got)
		}
		found := false
		for _, request := range f.requests {
			if strings.HasPrefix(fmt.Sprint(request["query"]), "count(") {
				found = true
				if request["query"] != "count(histogram_count(latency))" {
					t.Fatalf("query=%v", request)
				}
			}
		}
		if !found {
			t.Fatal("native histogram was not checked")
		}
	}
	for _, status := range []int{401, 403, 503} {
		f := &metricDescriptionFixture{nativeStatus: status, metadata: map[string]string{"latency": metricDefinition("histogram")}}
		res, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{"latency"}})
		if status == 503 {
			if err != nil || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "native histogram discovery failed") {
				t.Fatalf("res=%+v err=%v", res, err)
			}
		} else if err == nil {
			t.Fatal("native probe auth failure swallowed")
		}
	}
}

func TestDescribeMetricInvalidNativeCount(t *testing.T) {
	for _, count := range []string{"+Inf", "NaN", "-1", "0.5", "bad"} {
		got := describeOne(t, &metricDescriptionFixture{nativeCount: count, metadata: map[string]string{"latency": metricDefinition("histogram")}}, "latency")
		if got.RecommendedQuery != "" || !strings.Contains(strings.Join(got.Notes, " "), "native histogram discovery failed") {
			t.Fatalf("count=%q: %+v", count, got)
		}
	}
}

func TestDescribeMetricTotalResponseIncludesDatasourceNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/prom_metadata" {
			fmt.Fprint(w, `{"status":"success","data":{}}`)
		} else {
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	cfg := models.Config{APIBaseURL: srv.URL, DatasourceName: strings.Repeat("x", 256<<10), TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
	res, _, err := NewDescribeMetricHandler(srv.Client(), cfg)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{"up"}})
	if err == nil || !strings.Contains(err.Error(), "exceeds 256 KiB") || res != nil {
		t.Fatalf("oversized response was not refused: err=%v", err)
	}
}

func TestDescribeMetricBoundsInvalidNameError(t *testing.T) {
	f := &metricDescriptionFixture{}
	_, _, err := f.handler(t)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{strings.Repeat("x", 256<<10)}})
	if err == nil || len(err.Error()) > 1024 || len(f.requests) != 0 {
		t.Fatal("invalid name must fail before I/O with a bounded diagnostic")
	}
}

func TestDescribeMetricLabelKeysComeFromSeries(t *testing.T) {
	for _, tc := range []struct {
		name, series string
		want         []string
		wantType     string
	}{
		{"system_cpu_time_seconds_total", `[{"metric":{"cpu":"0","state":"idle"},"value":[1700000000,"1"]},{"metric":{"cpu":"1","mode":"x"},"value":[1700000000,"1"]}]`, []string{"cpu", "mode", "state"}, "counter"},
		{"go_gc_duration_seconds", `[{"metric":{"quantile":"0.5"},"value":[1700000000,"1"]}]`, []string{"quantile"}, "summary"},
		{"absent_metric", `[]`, []string{}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				switch query := fmt.Sprint(body["query"]); {
				case r.URL.Path == "/apm/labels":
					// The label endpoint can answer with keys unrelated to the metric.
					fmt.Fprint(w, `["span_kind","status_code"]`)
				case r.URL.Path == "/prom_query_instant" && strings.Contains(query, "last_over_time("+tc.name+"["):
					fmt.Fprint(w, tc.series)
				default:
					fmt.Fprint(w, `[]`)
				}
			}))
			t.Cleanup(srv.Close)
			cfg := models.Config{APIBaseURL: srv.URL, TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}}
			res, _, err := NewDescribeMetricHandler(srv.Client(), cfg)(context.Background(), nil, DescribeMetricArgs{MetricNames: []string{tc.name}})
			if err != nil {
				t.Fatal(err)
			}
			var report DescribeMetricResult
			if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &report); err != nil {
				t.Fatal(err)
			}
			got := report.Metrics[0]
			if !slices.Equal(got.LabelKeys, tc.want) || got.Type != tc.wantType {
				t.Fatalf("label keys must come from the metric's series: %+v", got)
			}
		})
	}
}

func TestDescribeMetricSemanticLabelsSurviveOutputCap(t *testing.T) {
	for _, tc := range []struct{ name, discovered, key, kind, query string }{
		{"custom_latency", "custom_latency", "quantile", "summary", `custom_latency{quantile!=""}`},
		{"custom_duration_bucket", "custom_duration_bucket", "le", "histogram", "histogram_quantile(0.95, sum by (le) (rate(custom_duration_bucket[5m])))"},
		{"custom_duration_sum", "custom_duration_bucket", "le", "histogram", "histogram_quantile(0.95, sum by (le) (rate(custom_duration_bucket[5m])))"},
		{"custom_duration_count", "custom_duration_bucket", "le", "histogram", "histogram_quantile(0.95, sum by (le) (rate(custom_duration_bucket[5m])))"},
	} {
		for _, extra := range []int{18, 19, 20, 21} {
			t.Run(fmt.Sprintf("%s/extra=%d", tc.name, extra), func(t *testing.T) {
				labels := []string{tc.key}
				for i := 0; i < extra; i++ {
					labels = append(labels, fmt.Sprintf("a_label_%02d", i))
				}
				raw, err := json.Marshal(labels)
				if err != nil {
					t.Fatal(err)
				}
				f := &metricDescriptionFixture{status: 404, labels: map[string]string{tc.discovered: string(raw)}, probe: probeFixture(`"10","12","11","13"`)}
				got := describeOne(t, f, tc.name)
				if len(got.LabelKeys) > 20 {
					t.Errorf("output exceeds label cap: %d", len(got.LabelKeys))
				}
				if got.Type != tc.kind || got.RecommendedQuery != tc.query {
					t.Fatalf("observed %s must determine semantics despite display cap: type=%s query=%q notes=%v", tc.key, got.Type, got.RecommendedQuery, got.Notes)
				}
			})
		}
	}
}
