package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTracesHandler_EmptyResultIncludesRetryGuidance(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointTracesQueryRange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return traceJSONResponse(http.StatusOK, `{"data":{"result":[]}}`), nil
	})}

	result, _, err := NewGetTracesHandler(client, traceGuidanceConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		GetTracesArgs{
			TracejsonQuery: []map[string]interface{}{{
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

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	guidance, ok := payload["l9_guidance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_guidance in empty response, got %#v", payload)
	}
	text := tracesGuidanceText(guidance)
	for _, want := range []string{"No traces found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(text, want) {
			t.Fatalf("guidance %q missing %q", text, want)
		}
	}
}

func TestGetServiceTracesHandler_EmptyServiceResultIncludesRetryGuidance(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointTracesQueryRange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return traceJSONResponse(http.StatusOK, `{"data":{"result":[]}}`), nil
	})}

	result, _, err := GetServiceTracesHandler(client, traceGuidanceConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		GetServiceTracesArgs{ServiceName: "checkut", Env: "prod", LookbackMinutes: 60},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var payload TraceQueryResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	for _, want := range []string{"No traces found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(payload.Message, want) {
			t.Fatalf("message %q missing %q", payload.Message, want)
		}
	}
}

func TestGetExceptionsHandler_EmptyResultIncludesRetryGuidance(t *testing.T) {
	endTime := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-60 * time.Minute)
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointPromQueryInstant {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return traceJSONResponse(http.StatusOK, `[]`), nil
	})}

	result, _, err := NewGetExceptionsHandler(client, traceGuidanceConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		GetExceptionsArgs{
			StartTimeISO: startTime.Format(time.RFC3339),
			EndTimeISO:   endTime.Format(time.RFC3339),
			ServiceName:  "checkut",
			Env:          "prod",
		},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	hint, _ := payload["hint"].(string)
	for _, want := range []string{"No exceptions found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}
}

func TestTraceUpstreamTimeoutAndRateLimitGuidance(t *testing.T) {
	tests := []struct {
		status int
		want   []string
	}{
		{status: http.StatusRequestTimeout, want: []string{"narrowing the time range", "service filter"}},
		{status: http.StatusTooManyRequests, want: []string{"wait", "narrower time range"}},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.status), func(t *testing.T) {
			message := newTraceHTTPError(&http.Response{StatusCode: test.status, Header: http.Header{}}).Error()
			for _, want := range test.want {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q missing %q", message, want)
				}
			}
		})
	}
}

func TestGetTracesHandler_TruncationUsesConfiguredMax(t *testing.T) {
	items := make([]any, 0, 100)
	for i := 0; i < 100; i++ {
		items = append(items, map[string]any{"TraceId": "0123456789abcdef0123456789abcdef"})
	}
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{"result": items},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointTracesQueryRange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return traceJSONResponse(http.StatusOK, string(body)), nil
	})}
	cfg := traceGuidanceConfig("https://last9.example")
	cfg.MaxGetTracesEntries = 100

	result, _, err := NewGetTracesHandler(client, cfg)(
		context.Background(),
		&mcp.CallToolRequest{},
		GetTracesArgs{
			TracejsonQuery: []map[string]interface{}{{
				"type":  "filter",
				"query": map[string]interface{}{"$eq": []interface{}{"TraceId", "0123456789abcdef0123456789abcdef"}},
			}},
			LookbackMinutes: 60,
		},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	guidance, ok := payload["l9_guidance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_guidance in truncated response, got %#v", payload)
	}
	text := tracesGuidanceText(guidance)
	for _, want := range []string{"Returned the first 100 traces", "start_time_iso", "end_time_iso", "add more selective filters"} {
		if !strings.Contains(text, want) {
			t.Fatalf("guidance %q missing %q", text, want)
		}
	}
	if strings.Contains(text, "5,000") {
		t.Fatalf("guidance must use configured max, got %q", text)
	}
}

func traceGuidanceConfig(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		Region:     "ap-south-1",
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

func tracesGuidanceText(guidance map[string]interface{}) string {
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

func traceJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
