package logs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	attributeScanStart = int64(1767225600) // 2026-01-01T00:00:00Z
	attributeScanEnd   = int64(1767312000) // 2026-01-02T00:00:00Z
)

func TestGetLogAttributesForPipelineScansEarlierSlices(t *testing.T) {
	var seriesCalls atomic.Int32
	server := attributeScanServer(t, func(start, end int64, series bool) string {
		if series {
			seriesCalls.Add(1)
			if start == attributeScanStart && end == attributeScanStart+3600 {
				return `{"status":"success","data":[{"service":"batch","backend_status_code":"200"}]}`
			}
			return `{"status":"success","data":[{"service":"","severity":"","body":""}]}`
		}
		if start == attributeScanStart && end == attributeScanStart+3600 {
			return streamsAPIResponse([]logValue{{Timestamp: "1767227400000000000", Message: `{"request_id":"abc"}`}})
		}
		return streamsAPIResponse(nil)
	})
	t.Cleanup(server.Close)

	result, _, err := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
			Pipeline:     []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
			StartTimeISO: "2026-01-01T00:00:00Z",
			EndTimeISO:   "2026-01-02T00:00:00Z",
		})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"backend_status_code", "request_id", "2026-01-01T00:00:00Z", "2026-01-01T01:00:00Z"} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q: %s", want, text)
		}
	}
	if got := seriesCalls.Load(); got != 24 {
		t.Fatalf("series calls = %d, want 24 before finding the oldest slice", got)
	}
}

func TestGetLogAttributesForPipelineReportsNoMatchingRowsWithinScanBound(t *testing.T) {
	var calls atomic.Int32
	server := attributeScanServer(t, func(_, _ int64, series bool) string {
		calls.Add(1)
		if series {
			return `{"status":"success","data":[{"service":"","severity":"","body":""}]}`
		}
		return streamsAPIResponse(nil)
	})
	t.Cleanup(server.Close)

	result, _, err := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
			Pipeline:     []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
			StartTimeISO: "2026-01-01T00:00:00Z",
			EndTimeISO:   "2026-01-03T00:00:00Z",
		})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "No rows matched the pipeline") || !strings.Contains(text, "at most 60 minutes per slice") {
		t.Fatalf("missing explicit empty result: %s", text)
	}
	if !strings.Contains(text, "2026-01-02T00:00:00Z") || strings.Contains(text, "No rows matched the pipeline in 2026-01-01T00:00:00Z") {
		t.Fatalf("empty result must report only the bounded 24-hour scan: %s", text)
	}
	if got := calls.Load(); got != 48 {
		t.Fatalf("backend calls = %d, want 48 (two calls for each of 24 slices)", got)
	}
}

func TestGetLogAttributesForPipelineReportsEqualBounds(t *testing.T) {
	var calls atomic.Int32
	server := attributeScanServer(t, func(_, _ int64, _ bool) string {
		calls.Add(1)
		return `{"status":"success","data":[]}`
	})
	t.Cleanup(server.Close)

	result, _, err := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
			Pipeline:     []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
			StartTimeISO: "2026-01-01T00:00:00Z",
			EndTimeISO:   "2026-01-01T00:00:00Z",
		})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if got := strings.Count(text, "2026-01-01T00:00:00Z"); got != 4 {
		t.Fatalf("equal-bounds result mentions requested instant %d times, want 4: %s", got, text)
	}
	if strings.Contains(text, "1970-01-01T00:00:00Z") {
		t.Fatalf("equal-bounds result reports an unscanned epoch window: %s", text)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("backend calls = %d, want 0 for an empty window", got)
	}
}

func TestGetServiceLogsStatusDiscoveryScansEarlierSlices(t *testing.T) {
	server := attributeScanServer(t, func(start, end int64, series bool) string {
		if series && start == attributeScanStart && end == attributeScanStart+3600 {
			return `{"status":"success","data":[{"service":"batch","status_code":"500"}]}`
		}
		if series {
			return `{"status":"success","data":[{"service":"","severity":"","body":""}]}`
		}
		if start == attributeScanStart && end == attributeScanStart+3600 {
			return streamsAPIResponse([]logValue{{Timestamp: "1767227400000000000", Message: "failed"}})
		}
		return streamsAPIResponse(nil)
	})
	t.Cleanup(server.Close)

	field, _, err := resolveHTTPStatusField(context.Background(), server.Client(), testAttrConfig(server.URL), GetServiceLogsArgs{
		ServiceName: "batch",
	}, unixTime(attributeScanStart), unixTime(attributeScanEnd), "")
	if err != nil {
		t.Fatal(err)
	}
	if field != "attributes['status_code']" {
		t.Fatalf("field = %q", field)
	}
}

func TestGetLogAttributesScansEarlierSlices(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		w.Header().Set("Content-Type", "application/json")
		if start == attributeScanStart {
			_, _ = io.WriteString(w, `{"status":"success","data":["ServiceName","status_code"]}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","data":[]}`)
	}))
	t.Cleanup(server.Close)

	result, _, err := NewGetLogAttributesHandler(server.Client(), testAttrConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetLogAttributesArgs{
			StartTimeISO: "2026-01-01T00:00:00Z",
			EndTimeISO:   "2026-01-02T00:00:00Z",
		})
	if err != nil {
		t.Fatal(err)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "status_code") || !strings.Contains(text, "2026-01-01 00:00:00 to 2026-01-01 01:00:00") {
		t.Fatalf("unexpected result: %s", text)
	}
	if got := calls.Load(); got != 24 {
		t.Fatalf("labels calls = %d, want 24", got)
	}
}

func TestGetLogAttributesReportsEqualBounds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":[]}`)
	}))
	t.Cleanup(server.Close)

	result, _, err := NewGetLogAttributesHandler(server.Client(), testAttrConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetLogAttributesArgs{
			StartTimeISO: "2026-01-01T00:00:00Z",
			EndTimeISO:   "2026-01-01T00:00:00Z",
		})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if strings.Contains(text, "1970-01-01") {
		t.Fatalf("equal-bounds result reports an unscanned epoch window: %s", text)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("backend calls = %d, want 0 for an empty window", got)
	}
}

func attributeScanServer(t *testing.T, response func(start, end int64, series bool) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == constants.EndpointLogsSeries {
			start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
			end, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
			_, _ = io.WriteString(w, response(start, end, true))
			return
		}
		start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		end, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
		_, _ = io.WriteString(w, response(start, end, false))
	}))
}

func unixTime(sec int64) time.Time {
	return time.Unix(sec, 0)
}
