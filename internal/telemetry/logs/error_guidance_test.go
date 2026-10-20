package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsHandler_EmptyResultIncludesRetryGuidance(t *testing.T) {
	client := &http.Client{Transport: logsRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointLogsQueryRange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"streams","result":[]}}`), nil
	})}

	result, _, err := NewGetLogsHandler(client, testLogsConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		GetLogsArgs{
			LogjsonQuery: []map[string]interface{}{{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkut"}},
						map[string]interface{}{"$eq": []interface{}{"resources['deployment.environment']", "prod"}},
					},
				},
			}},
			LookbackMinutes: 60,
		},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	payload := parseToolJSONResult(t, result)
	guidance, ok := payload["l9_guidance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_guidance in empty response, got %#v", payload)
	}
	text := guidanceText(guidance)
	for _, want := range []string{"No logs found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "env"} {
		if !strings.Contains(text, want) {
			t.Fatalf("guidance %q missing %q", text, want)
		}
	}
}

func TestGetServiceLogsHandler_EmptyResultIncludesRetryGuidance(t *testing.T) {
	client := &http.Client{Transport: logsRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointLogsQueryRange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"streams","result":[]}}`), nil
	})}

	result, _, err := NewGetServiceLogsHandler(client, testLogsConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		GetServiceLogsArgs{ServiceName: "checkut", Env: "prod", LookbackMinutes: 60},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	hint, _ := payload["hint"].(string)
	for _, want := range []string{"No logs found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}
}

func TestGetServiceLogsHandler_TruncatedResultIncludesNextStepGuidance(t *testing.T) {
	total := 4832
	truncated := true
	response := &ServiceLogsResponse{
		Service:            "checkout",
		StartTime:          "2026-01-01T00:00:00Z",
		EndTime:            "2026-01-01T01:00:00Z",
		Count:              100,
		Logs:               make([]LogEntry, 100),
		TotalMatchingLines: &total,
		LogsTruncated:      &truncated,
	}

	addServiceLogsGuidance(response, GetServiceLogsArgs{ServiceName: "checkout", Limit: 100}, 100)

	if response.Hint == "" {
		t.Fatal("truncated service logs response must include a hint")
	}
	for _, want := range []string{"Returned 100 of 4,832 logs", "start_time_iso", "end_time_iso", "increase limit"} {
		if !strings.Contains(response.Hint, want) {
			t.Fatalf("hint %q missing %q", response.Hint, want)
		}
	}
}

func TestGetLogsHandler_LogSearchAggregateTruncationIncludesNextStepGuidance(t *testing.T) {
	rows := make([]map[string]any, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, map[string]any{
			"metric": map[string]any{"service": "checkout"},
			"values": []any{},
		})
	}
	body, err := json.Marshal(map[string]any{
		"query_result": map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "matrix", "result": rows},
		},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	client := &http.Client{Transport: logsRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointLogSearch {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, string(body)), nil
	})}
	cfg := testLogsConfig("https://last9.example")
	cfg.UseLogSearchAPI = true

	result, _, err := NewGetLogsHandler(client, cfg)(
		context.Background(),
		&mcp.CallToolRequest{},
		GetLogsArgs{LogjsonQuery: countAggregatePipeline(), Limit: 10},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	payload := parseToolJSONResult(t, result)
	marker, ok := payload["l9_result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected partial marker, got %#v", payload["l9_result"])
	}
	hint, _ := marker["hint"].(string)
	for _, want := range []string{"Returned", "log rows", "start_time_iso", "end_time_iso", "increase limit", "5,000"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}
}

func TestGetLogsHandler_LogSearchRawTruncationUsesServerCap(t *testing.T) {
	values := make([]any, 0, 5000)
	for i := 0; i < 5000; i++ {
		values = append(values, []any{"1700000000000000000", "line"})
	}
	body, err := json.Marshal(map[string]any{
		"query_result": map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "streams",
				"result": []any{
					map[string]any{
						"stream": map[string]any{"ServiceName": "checkout"},
						"values": values,
					},
				},
			},
		},
		"logs_truncated": true,
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	client := &http.Client{Transport: logsRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointLogSearch {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, string(body)), nil
	})}
	cfg := testLogsConfig("https://last9.example")
	cfg.UseLogSearchAPI = true
	cfg.MaxGetLogsEntries = 10000

	result, _, err := NewGetLogsHandler(client, cfg)(
		context.Background(),
		&mcp.CallToolRequest{},
		GetLogsArgs{
			LogjsonQuery: []map[string]interface{}{{
				"type":  "filter",
				"query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}},
			}},
		},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	payload := parseToolJSONResult(t, result)
	guidance, ok := payload["l9_guidance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_guidance in truncated response, got %#v", payload)
	}
	text := guidanceText(guidance)
	for _, want := range []string{"Returned the first 5,000 logs", "start_time_iso", "end_time_iso", "add more selective filters"} {
		if !strings.Contains(text, want) {
			t.Fatalf("guidance %q missing %q", text, want)
		}
	}
	if strings.Contains(text, "10,000") {
		t.Fatalf("guidance must not suggest configured limit beyond server cap: %q", text)
	}
}

func TestGetLogsHandler_LogSearchAggregateNullResultIncludesEmptyGuidance(t *testing.T) {
	client := &http.Client{Transport: logsRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointLogSearch {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"query_result":{"status":"success","data":{"resultType":"matrix","result":null}}
		}`), nil
	})}
	cfg := testLogsConfig("https://last9.example")
	cfg.UseLogSearchAPI = true

	result, _, err := NewGetLogsHandler(client, cfg)(
		context.Background(),
		&mcp.CallToolRequest{},
		GetLogsArgs{LogjsonQuery: countAggregatePipeline(), Limit: 10},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	payload := parseToolJSONResult(t, result)
	guidance, ok := payload["l9_guidance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_guidance for null aggregate result, got %#v", payload)
	}
	text := guidanceText(guidance)
	for _, want := range []string{"No logs found", "widen", "6h", "did_you_mean"} {
		if !strings.Contains(text, want) {
			t.Fatalf("guidance %q missing %q", text, want)
		}
	}
}

func guidanceText(guidance map[string]interface{}) string {
	var parts []string
	for _, value := range guidance {
		switch value := value.(type) {
		case string:
			parts = append(parts, value)
		case []interface{}:
			for _, item := range value {
				parts = append(parts, item.(string))
			}
		}
	}
	return strings.Join(parts, " ")
}

type logsRoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn logsRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
