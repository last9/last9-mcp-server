package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCompileServiceLogAttributeField(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		want    string
		wantErr string
	}{
		{name: "bracket attribute", field: "attributes['user_id']", want: "attributes['user_id']"},
		{name: "simple name wraps as attribute", field: "user_id", want: "attributes['user_id']"},
		{name: "canonical ServiceName", field: "ServiceName", want: "ServiceName"},
		{name: "double quotes are invalid syntax", field: `attributes["user_id"]`, wantErr: "single quotes"},
		{name: "flat resource_ prefix is invalid syntax", field: "resource_department", wantErr: "resources["},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compileServiceLogAttributeField(tt.field, "attribute_filters[0].field")
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected syntax error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q missing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestApplyServiceLogsStructuredFilters_CompilesEquality(t *testing.T) {
	base := buildServiceLogsQuery("api", nil, nil)
	extra := []map[string]interface{}{
		{"$eq": []interface{}{"attributes['user_id']", "abc"}},
	}
	got, err := applyServiceLogsStructuredFilters(base, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `attributes['user_id']`) || !strings.Contains(s, `"abc"`) {
		t.Fatalf("compiled filter missing attribute equality: %s", s)
	}
	if strings.Contains(s, `"type":"parse"`) {
		t.Fatalf("indexed attribute filter must not add parse: %s", s)
	}
}

func TestApplyServiceLogsStructuredFilters_MissingAndIsError(t *testing.T) {
	_, err := applyServiceLogsStructuredFilters(nil, []map[string]interface{}{
		{"$eq": []interface{}{"attributes['status_code']", "500"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error when filters cannot be applied")
	}
}

func TestIsHTTPStatusLikeAttributeRejectsJobStatusCode(t *testing.T) {
	if isHTTPStatusLikeAttribute(LogAttribute{Name: "job_status_code", FilterField: "attributes['job_status_code']"}) {
		t.Fatal("job_status_code must not count as HTTP status")
	}
	if !isHTTPStatusLikeAttribute(LogAttribute{Name: "status_code", FilterField: "attributes['status_code']"}) {
		t.Fatal("status_code should count as HTTP status")
	}
	if !isHTTPStatusLikeAttribute(LogAttribute{Name: "http.status_code", FilterField: "attributes['http.status_code']"}) {
		t.Fatal("http.status_code should count as HTTP status")
	}
}

func TestGetServiceLogs_HTTP5xxUsesDiscoveredField(t *testing.T) {
	var queryRangeHits atomic.Int32
	var capturedPipeline string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, constants.EndpointLogsSeries):
			_, _ = w.Write([]byte(`{"status":"success","data":[{"service":"checkout","status_code":"500"}]}`))
		case r.URL.Path == constants.EndpointLogsQueryRange:
			raw, _ := io.ReadAll(r.Body)
			if strings.Contains(string(raw), `"$regex"`) {
				queryRangeHits.Add(1)
				capturedPipeline = string(raw)
			}
			_, _ = w.Write([]byte(streamsAPIResponse([]logValue{{Timestamp: "1000000000", Message: "boom"}})))
		default:
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
		}
	}))
	t.Cleanup(server.Close)

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "checkout",
		Env:             "production",
		LookbackMinutes: 5,
		HTTPStatusClass: "5xx",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if queryRangeHits.Load() < 1 {
		t.Fatal("expected query_range after successful status-field discovery")
	}
	if !strings.Contains(capturedPipeline, `attributes['status_code']`) {
		t.Fatalf("compiled pipeline missing discovered status field: %s", capturedPipeline)
	}
	if !strings.Contains(capturedPipeline, `"$regex"`) || !strings.Contains(capturedPipeline, `"^5"`) {
		t.Fatalf("compiled pipeline missing 5xx regex: %s", capturedPipeline)
	}
	if !strings.Contains(capturedPipeline, `"checkout"`) {
		t.Fatalf("compiled pipeline missing service filter: %s", capturedPipeline)
	}

	payload := parseServiceLogsToolResult(t, result)
	if payload["count"] != float64(1) {
		t.Fatalf("expected 1 log, got %#v", payload["count"])
	}
	if payload["http_status_field"] != "attributes['status_code']" {
		t.Fatalf("expected discovered http_status_field in response, got %#v", payload["http_status_field"])
	}
}

func TestGetServiceLogs_AmbiguousStatusFieldsMakeZeroQueryRange(t *testing.T) {
	var queryRangeHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == constants.EndpointLogsQueryRange {
			raw, _ := io.ReadAll(r.Body)
			if strings.Contains(string(raw), `"$regex"`) {
				queryRangeHits.Add(1)
			}
		}
		if strings.Contains(r.URL.Path, constants.EndpointLogsSeries) {
			_, _ = w.Write([]byte(`{"status":"success","data":[{"http.status_code":"500","http_status_code":"500"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(server.Close)

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "checkout",
		LookbackMinutes: 5,
		HTTPStatusClass: "5xx",
	})
	if err == nil {
		t.Fatal("expected tool error asking for http_status_field")
	}
	if !strings.Contains(err.Error(), "http_status_field") {
		t.Fatalf("error should ask for http_status_field, got %v", err)
	}
	if got := queryRangeHits.Load(); got != 0 {
		t.Fatalf("ambiguous discovery made %d query_range calls, want 0", got)
	}
}

func TestGetServiceLogs_InvalidAttributeSyntaxMakesZeroUpstream(t *testing.T) {
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "checkout",
		LookbackMinutes: 5,
		AttributeFilters: []ServiceLogAttributeFilter{
			{Field: `attributes["user_id"]`, Value: "abc"},
		},
	})
	if err == nil {
		t.Fatal("expected local syntax error")
	}
	if !strings.Contains(err.Error(), "single quotes") {
		t.Fatalf("error %v should mention single quotes", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("invalid syntax made %d upstream requests, want 0", got)
	}
}

func TestGetServiceLogs_ExplicitStatusFieldSkipsDiscovery(t *testing.T) {
	var seriesHits, queryRangeHits atomic.Int32
	var capturedPipeline string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, constants.EndpointLogsSeries) {
			seriesHits.Add(1)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
			return
		}
		if r.URL.Path == constants.EndpointLogsQueryRange {
			queryRangeHits.Add(1)
			raw, _ := io.ReadAll(r.Body)
			capturedPipeline = string(raw)
			_, _ = w.Write([]byte(streamsAPIResponse([]logValue{{Timestamp: "1", Message: "ok"}})))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	t.Cleanup(server.Close)

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "checkout",
		LookbackMinutes: 5,
		HTTPStatusCode:  "401",
		HTTPStatusField: "attributes['http.status_code']",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got := seriesHits.Load(); got != 0 {
		t.Fatalf("explicit field should skip series discovery, got %d", got)
	}
	if queryRangeHits.Load() < 1 {
		t.Fatal("expected query_range")
	}
	if !strings.Contains(capturedPipeline, `attributes['http.status_code']`) || !strings.Contains(capturedPipeline, `"401"`) {
		t.Fatalf("pipeline missing explicit 401 filter: %s", capturedPipeline)
	}
}

func parseServiceLogsToolResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected tool content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	return payload
}
