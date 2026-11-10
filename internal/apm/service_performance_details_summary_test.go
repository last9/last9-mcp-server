package apm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/constants"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// perfFixtureLabels sizes each sub-query's series like a busy service.
func perfFixtureLabels(query string) []map[string]string {
	codes := func(n int, prefix string) []map[string]string {
		out := make([]map[string]string, n)
		for i := range out {
			out[i] = map[string]string{"http_status_code": fmt.Sprintf("%s%02d", prefix, i)}
		}
		return out
	}
	switch {
	case strings.Contains(query, "by (quantile)"):
		var out []map[string]string
		for _, q := range []string{"p50", "p90", "p95", "p99", "avg", "max"} {
			out = append(out, map[string]string{"quantile": q})
		}
		return out
	case strings.Contains(query, "by (service_name, http_status_code)"):
		return codes(8, "5")
	case strings.Contains(query, "by (http_status_code)"):
		return codes(12, "2")
	default:
		return []map[string]string{{}}
	}
}

// perfFixtureValue steps up 4x at shiftAt so every series has one level shift.
func perfFixtureValue(series int, ts, shiftAt int64) float64 {
	v := float64(series+1) * (1 + float64(ts%7)/100)
	if ts >= shiftAt {
		v *= 4
	}
	return v
}

func newPerfFixtureServer(t *testing.T, shiftAt int64, topN int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
			Window    int64  `json:"window"`
		}
		_ = json.Unmarshal(body, &payload)

		if r.URL.Path == constants.EndpointPromQueryInstant {
			var rows []map[string]any
			for i := 0; i < topN+5; i++ {
				rows = append(rows, map[string]any{
					"metric": map[string]string{
						"span_name":      fmt.Sprintf("GET /api/v1/customers/{customer_id}/orders/{order_id}/items-%02d", i),
						"span_kind":      "SPAN_KIND_SERVER",
						"exception_type": fmt.Sprintf("io.grpc.StatusRuntimeException.Unavailable%02d", i),
					},
					"value": []any{payload.Timestamp, fmt.Sprintf("%d", 1000-i)},
				})
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}

		const points = perfDetailsMaxDataPoints
		start := payload.Timestamp - payload.Window
		step := payload.Window / points
		var resp []map[string]any
		for i, labels := range perfFixtureLabels(payload.Query) {
			values := make([][]any, 0, points+1)
			for ts := start; ts <= payload.Timestamp; ts += step {
				values = append(values, []any{ts, fmt.Sprintf("%g", perfFixtureValue(i, ts, shiftAt))})
			}
			resp = append(resp, map[string]any{"metric": labels, "values": values})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func callPerfDetails(t *testing.T, srv *httptest.Server, args ServicePerformanceDetailsArgs) string {
	t.Helper()
	result, _, err := NewServicePerformanceDetailsHandler(srv.Client(), apmTestConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result.Content[0].(*mcp.TextContent).Text
}

func nearestRankFromFull(values []float64, p float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[int(math.Ceil(p/100*float64(len(sorted))))-1]
}

func TestServicePerformanceDetails_SummaryByDefaultMatchesFullSeries(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Hour)
	start := end.Add(-maxServicePerformanceWindowDays * 24 * time.Hour)
	shiftAt := start.Add(end.Sub(start) / 2).Unix()
	srv := newPerfFixtureServer(t, shiftAt, perfDetailsDefaultTopN)
	defer srv.Close()

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "checkout",
		Env:          "prod",
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
	}
	summaryText := callPerfDetails(t, srv, args)
	args.IncludeSeries = true
	args.TopN = perfSummaryDefaultTopN // the full payload otherwise defaults to perfDetailsDefaultTopN
	fullText := callPerfDetails(t, srv, args)

	const maxSummaryChars = 6000
	if len(summaryText) > maxSummaryChars {
		t.Errorf("summary is %d chars, want <= %d (full series is %d chars)", len(summaryText), maxSummaryChars, len(fullText))
	}

	t.Logf("summary %d chars, full series %d chars", len(summaryText), len(fullText))

	var summary ServicePerformanceSummary
	if err := json.Unmarshal([]byte(summaryText), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.FullSeries != perfSummaryFullSeriesNote || !strings.Contains(summaryText, "include_series=true") {
		t.Errorf("summary must say the full series is available via include_series=true, got %q", summary.FullSeries)
	}
	if len(summary.TopErrors) != perfSummaryDefaultTopN || len(summary.TopOperations.ByResponseTime) != perfSummaryDefaultTopN {
		t.Errorf("unset top_n must default to %d in summary mode, got %d top_errors", perfSummaryDefaultTopN, len(summary.TopErrors))
	}
	if summary.SeriesOmitted["throughput"] != 12-perfSummaryMaxSeries || summary.SeriesOmitted["error_rate"] != 8-perfSummaryMaxSeries {
		t.Errorf("series_omitted = %v", summary.SeriesOmitted)
	}

	dec := json.NewDecoder(strings.NewReader(fullText))
	dec.DisallowUnknownFields()
	var full ServicePerformanceDetails
	if err := dec.Decode(&full); err != nil {
		t.Fatalf("full-series payload must keep the previous shape: %v", err)
	}
	if len(full.ApdexScore) != 1 || len(full.ApdexScore[0].Values) < 1000 {
		t.Fatalf("fixture should merge a long apdex series, got %d series", len(full.ApdexScore))
	}

	var apdexValues []float64
	var shiftTs uint64
	for _, p := range full.ApdexScore[0].Values {
		apdexValues = append(apdexValues, p.Value)
		if shiftTs == 0 && int64(p.Timestamp) >= shiftAt {
			shiftTs = p.Timestamp
		}
	}
	got := summary.ApdexScore[0]
	for _, c := range []struct {
		name string
		got  float64
		p    float64
	}{{"p50", got.P50, 50}, {"p90", got.P90, 90}, {"p99", got.P99, 99}} {
		if want := roundSig(nearestRankFromFull(apdexValues, c.p)); c.got != want {
			t.Errorf("apdex %s = %v, want %v from the full series", c.name, c.got, want)
		}
	}
	if got.Points != len(apdexValues) {
		t.Errorf("apdex points = %d, want %d", got.Points, len(apdexValues))
	}
	if len(got.ChangePoints) != 1 || got.ChangePoints[0].Timestamp != shiftTs {
		t.Errorf("apdex change points = %+v, want one at %d", got.ChangePoints, shiftTs)
	}

	want, _ := json.Marshal(summarizePerformanceDetails(full))
	if !bytes.Equal(want, []byte(summaryText)) {
		t.Errorf("default response differs from the summary of the full-series response:\n got %s\nwant %s", summaryText, want)
	}
}

func TestServicePerformanceDetails_NonPositiveTopNUsesDefault(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Minute)
	srv := newPerfFixtureServer(t, end.Add(-30*time.Minute).Unix(), perfDetailsDefaultTopN)
	defer srv.Close()

	for _, topN := range []int{0, -1} {
		text := callPerfDetails(t, srv, ServicePerformanceDetailsArgs{
			ServiceName:  "checkout",
			StartTimeISO: end.Add(-time.Hour).Format(time.RFC3339),
			EndTimeISO:   end.Format(time.RFC3339),
			TopN:         topN,
		})
		var summary ServicePerformanceSummary
		if err := json.Unmarshal([]byte(text), &summary); err != nil {
			t.Fatalf("top_n=%d: decode: %v", topN, err)
		}
		if len(summary.TopErrors) != perfSummaryDefaultTopN {
			t.Errorf("top_n=%d: got %d top_errors, want default %d", topN, len(summary.TopErrors), perfSummaryDefaultTopN)
		}
	}
}

func TestServicePerformanceDetails_IncludeSeriesKeepsPreviousPayload(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Minute)
	srv := newPerfFixtureServer(t, end.Add(-30*time.Minute).Unix(), 3)
	defer srv.Close()

	text := callPerfDetails(t, srv, ServicePerformanceDetailsArgs{
		ServiceName:   "checkout",
		StartTimeISO:  end.Add(-time.Hour).Format(time.RFC3339),
		EndTimeISO:    end.Format(time.RFC3339),
		IncludeSeries: true,
	})
	var full ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &full); err != nil {
		t.Fatalf("decode: %v", err)
	}
	reencoded, _ := json.Marshal(full)
	if string(reencoded) != text {
		t.Errorf("include_series=true must return the ServicePerformanceDetails payload verbatim:\n got %s\nwant %s", text, reencoded)
	}
	if strings.Contains(text, "full_series") {
		t.Error("full-series payload must not carry the summary marker")
	}
}

func TestServicePerformanceDetails_SummaryExcludesAvailabilityNoData(t *testing.T) {
	for _, tc := range []struct {
		name        string
		values      []string
		wantPoints  int
		wantMissing int
	}{
		{"all_valid", []string{"100", "100", "100", "100", "100", "100"}, 6, 0},
		{"no_data_then_valid", []string{"-999", "-999", "-999", "100", "100", "100"}, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var payload struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(body, &payload)
				if !strings.Contains(payload.Query, "default -999") {
					_, _ = w.Write([]byte("[]"))
					return
				}
				values := make([][]any, len(tc.values))
				for i, v := range tc.values {
					values[i] = []any{int64(1792540800 + i*60), v}
				}
				_ = json.NewEncoder(w).Encode([]any{map[string]any{"metric": map[string]string{}, "values": values}})
			}))
			defer srv.Close()
			args := ServicePerformanceDetailsArgs{ServiceName: "checkout", StartTimeISO: "2026-10-21T00:00:00Z", EndTimeISO: "2026-10-21T01:00:00Z"}

			var summary ServicePerformanceSummary
			if err := json.Unmarshal([]byte(callPerfDetails(t, srv, args)), &summary); err != nil {
				t.Fatalf("decode summary: %v", err)
			}
			if len(summary.Availability) != 1 {
				t.Fatalf("availability = %+v, want one series", summary.Availability)
			}
			got := summary.Availability[0]
			if got.Points != tc.wantPoints || got.MissingPoints != tc.wantMissing || got.Mean != 100 || got.Min != 100 || len(got.ChangePoints) != 0 {
				t.Errorf("availability = %+v, want points=%d missing_points=%d, mean=min=100, no change points", got, tc.wantPoints, tc.wantMissing)
			}

			args.IncludeSeries = true
			var full ServicePerformanceDetails
			if err := json.Unmarshal([]byte(callPerfDetails(t, srv, args)), &full); err != nil {
				t.Fatalf("decode full series: %v", err)
			}
			if len(full.Availability) != 1 || len(full.Availability[0].Values) != len(tc.values) {
				t.Errorf("include_series availability = %+v, want all %d raw points", full.Availability, len(tc.values))
			}
		})
	}
}

func TestServicePerformanceDetailsArgs_TopNDescriptionStatesBothDefaults(t *testing.T) {
	schema, err := jsonschema.For[ServicePerformanceDetailsArgs](nil)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	got := schema.Properties["top_n"].Description
	if want := "hundreds of KB to MBs"; !strings.Contains(schema.Properties["include_series"].Description, want) {
		t.Errorf("include_series description must size the full payload as %q", want)
	}
	for _, want := range []string{
		fmt.Sprintf("default: %d", perfSummaryDefaultTopN),
		fmt.Sprintf("%d with include_series=true", perfDetailsDefaultTopN),
		"0 or less uses the default",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("top_n description %q missing %q", got, want)
		}
	}
	// The response nests the lists as top_operations.by_response_time/.by_error_rate.
	if strings.Contains(got, "top_operations_by_") {
		t.Errorf("top_n description %q names keys the response doesn't have", got)
	}
}
