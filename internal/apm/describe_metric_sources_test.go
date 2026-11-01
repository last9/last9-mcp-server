package apm

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDescribeMetricSources(t *testing.T) {
	for _, tt := range []struct{ name, labels, kind, source, unit string }{
		{"trace_service_response_time", `["quantile"]`, "gauge", "catalog", "milliseconds"},
		{"trace_endpoint_count", `[]`, "gauge", "catalog", "count"},
		{"last9_change_events", `["event_name"]`, "gauge", "catalog", ""},
		{"redis_memory_used", `[]`, "gauge", "receiver", "By"},
		{"redis_memory_used_bytes", `[]`, "gauge", "receiver", "By"},
		{"redis_commands_processed_total", `[]`, "counter", "receiver", "{command}"},
		{"redis_clients_connected", `[]`, "gauge", "receiver", "{client}"},
		{"requests_total", `[]`, "counter", "naming", ""},
		{"requests_created", `[]`, "counter", "naming", ""},
		{"latency_seconds_bucket", `["le"]`, "histogram", "naming", "seconds"},
		{"response_milliseconds", `["quantile"]`, "summary", "naming", "milliseconds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &metricDescriptionFixture{status: 404, labels: map[string]string{tt.name: tt.labels}}
			got := describeOne(t, f, tt.name)
			if got.Type != tt.kind || got.TypeSource != tt.source || got.Unit != tt.unit || got.Confidence == "" {
				t.Fatalf("%+v", got)
			}
			for _, req := range f.requests {
				if req["path"] == "/prom_metadata" {
					t.Fatal("resolved type depends on optional metadata")
				}
			}
			if got.Type == "gauge" && strings.Contains(got.RecommendedQuery, "rate(") {
				t.Fatalf("unsafe gauge: %+v", got)
			}
			if strings.HasPrefix(tt.name, "trace_") && strings.Contains(got.RecommendedQuery, "histogram_quantile") {
				t.Fatalf("unsafe trace: %+v", got)
			}
		})
	}
}

func TestDescribeMetricProbeSources(t *testing.T) {
	for _, tt := range []struct{ name, values, kind string }{
		{"rising", `"1","2","3","4","5","6"`, "counter"},
		{"reset", `"10","11","0","1","2","3"`, "counter"},
		{"reset between scrapes", `"100","110","2","3","4","5"`, "counter"},
		{"short reset", `"10","11","0","1"`, "counter"},
		{"gauge", `"10","12","11","13","12","11"`, "gauge"},
		{"falling", `"10","9","8","7","6","5"`, "gauge"},
		{"flat", `"3","3","3","3","3","3"`, "gauge"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &metricDescriptionFixture{status: 404, probe: probeFixture(tt.values)}
			got := describeOne(t, f, "load_value")
			if got.Type != tt.kind || got.TypeSource != "probe" || got.Confidence != "low" || !got.Inferred {
				t.Fatalf("%+v", got)
			}
			if strings.Contains(got.RecommendedQuery, "rate(") {
				t.Fatalf("sample shape cannot prove a counter: %+v", got)
			}
		})
	}
}

func probeFixture(values string) string {
	parts := strings.Split(values, ",")
	samples := make([]string, len(parts))
	for i, value := range parts {
		samples[i] = fmt.Sprintf("[%d,%s]", time.Now().Unix()-int64(len(parts)-1-i)*30, value)
	}
	return `[{"metric":{"job":"api"},"values":[` + strings.Join(samples, ",") + `]}]`
}

func TestDescribeMetricEmptyMetadata(t *testing.T) {
	got := describeOne(t, &metricDescriptionFixture{}, "unrecognized")
	if got.TypeSource != "unknown" || got.MetadataStatus != "metadata_unavailable" || !strings.Contains(strings.Join(got.Notes, " "), "until verified") {
		t.Fatalf("%+v", got)
	}
}

func TestDescribeMetricNamingUnitsAndFamilies(t *testing.T) {
	for _, unit := range []string{"seconds", "milliseconds", "bytes", "ratio", "percent"} {
		got := describeOne(t, &metricDescriptionFixture{}, "custom_"+unit)
		if got.Unit != unit || got.Type != "unknown" {
			t.Fatalf("%+v", got)
		}
	}
	for _, suffix := range []string{"_sum", "_count"} {
		f := &metricDescriptionFixture{status: 404, labels: map[string]string{"duration_seconds_bucket": `["le"]`}}
		got := describeOne(t, f, "duration_seconds"+suffix)
		if got.TypeSource != "naming" || got.Type != "histogram" || got.Unit != "seconds" || !strings.Contains(got.RecommendedQuery, "rate(duration_seconds_bucket[5m])") {
			t.Fatalf("%+v", got)
		}
	}
}

func TestDescribeMetricOptionalMetadata(t *testing.T) {
	for _, status := range []int{404, 429, 500, 501, 502, 503, 504} {
		got := describeOne(t, &metricDescriptionFixture{status: status}, "custom")
		want := "metadata_error"
		if status == 404 || status == 501 {
			want = "metadata_unavailable"
		}
		if got.TypeSource != "unknown" || got.MetadataStatus != want || got.RecommendedQuery != "" {
			t.Fatalf("status=%d: %+v", status, got)
		}
	}
	got := describeOne(t, &metricDescriptionFixture{metadata: map[string]string{"custom": metricDefinition("gauge")}}, "custom")
	if got.TypeSource != "metadata" || got.Confidence != "high" || got.MetadataStatus != "available" || got.RecommendedQuery != "custom" {
		t.Fatalf("%+v", got)
	}
}

func TestDescribeMetricProbeRequestBounds(t *testing.T) {
	f := &metricDescriptionFixture{probe: probeFixture(`"1","2","3","4"`)}
	got := describeOne(t, f, "untyped_metric")
	if got.TypeSource != "probe" {
		t.Fatalf("%+v", got)
	}
	probes := 0
	for _, req := range f.requests {
		if req["path"] != "/prom_query_instant" {
			continue
		}
		probes++
		want := fmt.Sprintf("(untyped_metric and topk(1, untyped_metric @ %.0f))[5m:30s]", req["timestamp"])
		if req["query"] != want {
			t.Fatalf("unbounded or unstable series selection: %v", req)
		}
	}
	if probes != 1 {
		t.Fatalf("probes=%d", probes)
	}
}

func TestDescribeMetricRejectsTraceHistogramComponents(t *testing.T) {
	f := &metricDescriptionFixture{labels: map[string]string{"trace_service_response_time_bucket": `["le"]`}, metadata: map[string]string{"trace_service_response_time": metricDefinition("histogram")}}
	got := describeOne(t, f, "trace_service_response_time_bucket")
	if got.Type != "unknown" || strings.Contains(got.RecommendedQuery, "histogram_quantile") {
		t.Fatalf("fabricated trace buckets: %+v", got)
	}
}

func TestDescribeMetricProbeResponseCap(t *testing.T) {
	raw := probeFixture(`"1","2","3","4"`)
	for _, n := range []int{(32 << 10) - 1, 32 << 10, (32 << 10) + 1} {
		f := &metricDescriptionFixture{probe: raw + strings.Repeat(" ", n-len(raw))}
		got := describeOne(t, f, "custom")
		want := "probe"
		if n > 32<<10 {
			want = "unknown"
		}
		if got.TypeSource != want {
			t.Errorf("size=%d: %+v", n, got)
		}
	}
}

func TestDescribeMetricEmptyDefinitionList(t *testing.T) {
	got := describeOne(t, &metricDescriptionFixture{metadata: map[string]string{"custom": `[]`}}, "custom")
	if got.Type != "unknown" || got.MetadataStatus != "metadata_unavailable" {
		t.Fatalf("%+v", got)
	}
}

func TestDescribeMetricGaugeEvidencePrecedesMetadata(t *testing.T) {
	f := &metricDescriptionFixture{probe: probeFixture(`"5","3","4","2"`), metadata: map[string]string{"custom": metricDefinition("counter")}}
	got := describeOne(t, f, "custom")
	if got.Type != "gauge" || got.TypeSource != "probe" || got.RecommendedQuery != "custom" {
		t.Fatalf("%+v", got)
	}
	for _, req := range f.requests {
		if req["path"] == "/prom_metadata" {
			t.Fatal("metadata was read after decisive gauge evidence")
		}
	}
}
