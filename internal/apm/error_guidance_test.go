package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServiceSummaryEmptyAndTruncatedResultsIncludeRetryGuidance(t *testing.T) {
	empty := runServiceSummaryGuidance(t, summaryPromFixture{}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 12, 20, 0, 0, time.UTC).Format(time.RFC3339),
		Env:          "^prod$",
	})

	for _, want := range []string{"No APM services found", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(empty.Hint, want) {
			t.Fatalf("empty hint %q missing %q", empty.Hint, want)
		}
	}

	request := make([]summaryPromSample, 0, 101)
	for i := 0; i < 101; i++ {
		request = append(request, summaryPromSample{service: "svc-" + strconv.Itoa(i), env: "prod", value: "1"})
	}
	truncated := runServiceSummaryGuidance(t, summaryPromFixture{request: request}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 12, 20, 0, 0, time.UTC).Format(time.RFC3339),
		Limit:        10,
	})
	if truncated.Hint == "" {
		t.Fatal("truncated response must include next-step guidance")
	}
	for _, want := range []string{"Returned 10 of 101 services", "start_time_iso", "end_time_iso", "increase limit"} {
		if !strings.Contains(truncated.Hint, want) {
			t.Fatalf("truncated hint %q missing %q", truncated.Hint, want)
		}
	}
}

func TestServiceOperationsSummaryEmptyResultIncludesRetryGuidance(t *testing.T) {
	client := emptyPromClient(t, constants.EndpointPromQueryInstant)

	result, _, err := NewServiceOperationsSummaryHandler(client, testDBConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		ServiceOperationsSummaryArgs{ServiceName: "checkut", Env: "prod", LookbackMinutes: 60},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	hint, _ := payload["hint"].(string)
	for _, want := range []string{"No operations found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}
}

func TestServiceDependencyGraphEmptyResultIncludesRetryGuidance(t *testing.T) {
	client := emptyPromClient(t, constants.EndpointPromQueryInstant)

	result, _, err := NewServiceDependencyGraphHandler(client, testDBConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		ServiceDependencyGraphArgs{ServiceName: "checkut", Env: "prod", LookbackMinutes: 60},
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	hint, _ := payload["hint"].(string)
	for _, want := range []string{"No dependency graph data found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}
}

func TestDatabaseEmptyResultsIncludeRetryGuidance(t *testing.T) {
	t.Run("slow queries", func(t *testing.T) {
		client := &http.Client{Transport: apmRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return apmJSONResponse(http.StatusOK, `{"data":{"result":[]}}`), nil
		})}

		result, _, err := NewGetDatabaseSlowQueriesHandler(client, testDBConfig("https://last9.example"))(
			context.Background(),
			&mcp.CallToolRequest{},
			GetDatabaseSlowQueriesArgs{ServiceName: "checkut", DBSystem: "postgresql", Env: "prod", LookbackMinutes: 60},
		)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		text := utils.GetTextContent(t, result)
		for _, want := range []string{"No slow database queries found", "checkut", "last 1h", "widen", "6h", "did_you_mean", "environment"} {
			if !strings.Contains(text, want) {
				t.Fatalf("text %q missing %q", text, want)
			}
		}
	})

	t.Run("query patterns", func(t *testing.T) {
		client := emptyPromClient(t, constants.EndpointPromQueryInstant)

		result, _, err := NewGetDatabaseQueriesHandler(client, testDBConfig("https://last9.example"))(
			context.Background(),
			&mcp.CallToolRequest{},
			GetDatabaseQueriesArgs{DBSystem: "postgresql", Env: "prod", LookbackMinutes: 60},
		)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		text := utils.GetTextContent(t, result)
		for _, want := range []string{"No query patterns found", "postgresql", "last 1h", "widen", "6h", "environment"} {
			if !strings.Contains(text, want) {
				t.Fatalf("text %q missing %q", text, want)
			}
		}
	})

	t.Run("server metrics", func(t *testing.T) {
		client := emptyPromClient(t, constants.EndpointPromLabelValues)

		result, _, err := NewGetDatabaseServerMetricsHandler(client, testDBConfig("https://last9.example"))(
			context.Background(),
			&mcp.CallToolRequest{},
			GetDatabaseServerMetricsArgs{DBSystem: "postgresql", LookbackMinutes: 60},
		)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		text := utils.GetTextContent(t, result)
		for _, want := range []string{"No database server-side metrics found", "postgresql", "last 1h", "widen", "6h"} {
			if !strings.Contains(text, want) {
				t.Fatalf("text %q missing %q", text, want)
			}
		}
	})
}

// Exact-match env tools echo the normalized literal (^prod$ -> prod), so
// their empty-result hints must name that literal, not the anchored input.
func TestExactEnvEmptyHintsNameNormalizedEnv(t *testing.T) {
	cfg := testDBConfig("https://last9.example")
	hintField := func(t *testing.T, result *mcp.CallToolResult) string {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		hint, _ := payload["hint"].(string)
		return hint
	}
	cases := []struct {
		name string
		call func(t *testing.T, env string) string
	}{
		{"operations summary", func(t *testing.T, env string) string {
			result, _, err := NewServiceOperationsSummaryHandler(emptyPromClient(t, constants.EndpointPromQueryInstant), cfg)(
				context.Background(), &mcp.CallToolRequest{},
				ServiceOperationsSummaryArgs{ServiceName: "checkout", Env: env, LookbackMinutes: 60})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			return hintField(t, result)
		}},
		{"dependency graph", func(t *testing.T, env string) string {
			result, _, err := NewServiceDependencyGraphHandler(emptyPromClient(t, constants.EndpointPromQueryInstant), cfg)(
				context.Background(), &mcp.CallToolRequest{},
				ServiceDependencyGraphArgs{ServiceName: "checkout", Env: env, LookbackMinutes: 60})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			return hintField(t, result)
		}},
		{"query patterns", func(t *testing.T, env string) string {
			result, _, err := NewGetDatabaseQueriesHandler(emptyPromClient(t, constants.EndpointPromQueryInstant), cfg)(
				context.Background(), &mcp.CallToolRequest{},
				GetDatabaseQueriesArgs{DBSystem: "postgresql", Env: env, LookbackMinutes: 60})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			return utils.GetTextContent(t, result)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+" anchored", func(t *testing.T) {
			hint := tc.call(t, "^prod$")
			if !strings.Contains(hint, "in environment prod ") || strings.Contains(hint, "^prod$") {
				t.Fatalf("hint %q must name the normalized env prod", hint)
			}
		})
		t.Run(tc.name+" all envs", func(t *testing.T) {
			hint := tc.call(t, ".*")
			if hint == "" || strings.Contains(hint, "in environment") || strings.Contains(hint, "verify the environment param") {
				t.Fatalf("hint %q must not scope to an environment for .*", hint)
			}
		})
	}
}

func emptyPromClient(t *testing.T, wantPath string) *http.Client {
	t.Helper()
	return &http.Client{Transport: apmRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != wantPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return apmJSONResponse(http.StatusOK, `[]`), nil
	})}
}

func runServiceSummaryGuidance(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs) ServiceSummaryResult {
	t.Helper()
	client := &http.Client{Transport: apmRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != constants.EndpointPromQueryInstant {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode instant request body: %v", err)
		}
		query := reqBody["query"].(string)
		switch {
		case strings.Contains(query, `http_status_code=~"5.*"`):
			return apmJSONResponse(http.StatusOK, promInstantJSON(t, fx.http5xx)), nil
		case strings.Contains(query, `http_status_code=~"4.*"`):
			return apmJSONResponse(http.StatusOK, promInstantJSON(t, fx.http4xx)), nil
		case strings.Contains(query, `grpc_status_code!=""`):
			return apmJSONResponse(http.StatusOK, promInstantJSON(t, fx.grpc)), nil
		default:
			return apmJSONResponse(http.StatusOK, promInstantJSON(t, fx.request)), nil
		}
	})}

	result, _, err := NewServiceSummaryHandler(client, testSummaryConfig("https://last9.example"))(
		context.Background(),
		&mcp.CallToolRequest{},
		args,
	)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var payload ServiceSummaryResult
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	return payload
}

type apmRoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn apmRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func apmJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
