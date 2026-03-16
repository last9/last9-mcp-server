package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetExceptionsHandler_UsesFrontendPromQueries(t *testing.T) {
	startTime := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	endTime := startTime.Add(30 * time.Minute)

	var instantCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(constants.HeaderXLast9APIToken); got != constants.BearerPrefix+"test-access-token" {
			t.Fatalf("unexpected auth header: %q", got)
		}

		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			instantCalls++

			var reqBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode instant request body: %v", err)
			}

			query := fmt.Sprintf("%v", reqBody["query"])
			if !strings.Contains(query, "trace_endpoint_count") ||
				!strings.Contains(query, "trace_client_count") ||
				!strings.Contains(query, "trace_internal_count") {
				t.Fatalf("instant query does not include expected trace_*_count metrics: %s", query)
			}
			if !strings.Contains(query, "service_name=~'checkout'") {
				t.Fatalf("service_name filter missing from instant query: %s", query)
			}
			if !strings.Contains(query, "span_name=~'POST \\\\/orders'") {
				t.Fatalf("span_name filter missing from instant query: %s", query)
			}
			if !strings.Contains(query, "env=~'prod'") {
				t.Fatalf("deployment_environment filter was not translated to env: %s", query)
			}
			if !strings.Contains(query, "exception_type!=''") {
				t.Fatalf("exception_type guard missing from instant query: %s", query)
			}
			if !strings.Contains(query, "sum by (exception_type, service_name, span_name, span_kind)") {
				t.Fatalf("instant query is missing expected grouping labels: %s", query)
			}
			if strings.Contains(query, "sum by (exception_type, service_name, span_name, span_kind, env)") {
				t.Fatalf("instant query should not include env in grouping labels: %s", query)
			}
			if !strings.Contains(query, "[30m]") {
				t.Fatalf("expected 30m range selector in instant query: %s", query)
			}

			if got := int64(reqBody["timestamp"].(float64)); got != endTime.Unix() {
				t.Fatalf("unexpected instant timestamp: got %d, want %d", got, endTime.Unix())
			}

			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fmt.Sprintf(`[
				{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_CLIENT"},"value":[%d,"3"]},
				{"metric":{"exception_type":"NullPointerException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_SERVER"},"value":[%d,"12"]}
			]`, endTime.Unix(), endTime.Unix()))

		case constants.EndpointPromQuery:
			t.Fatalf("did not expect range query")

		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "prom-user",
		PrometheusPassword: "prom-pass",
		OrgSlug:            "acme",
		ClusterID:          "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetExceptionsHandler(server.Client(), cfg)
	args := GetExceptionsArgs{
		StartTimeISO:          startTime.Format(time.RFC3339),
		EndTimeISO:            endTime.Format(time.RFC3339),
		ServiceName:           "checkout",
		SpanName:              "POST /orders",
		DeploymentEnvironment: "prod",
		Limit:                 10,
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if instantCalls != 1 {
		t.Fatalf("expected 1 instant query call, got %d", instantCalls)
	}

	text := utils.GetTextContent(t, result)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to decode tool response: %v", err)
	}

	if got := int(payload["count"].(float64)); got != 2 {
		t.Fatalf("unexpected top-level count: got %d, want 2", got)
	}

	exceptions, ok := payload["exceptions"].([]any)
	if !ok {
		t.Fatalf("expected exceptions array in response")
	}
	if len(exceptions) != 2 {
		t.Fatalf("unexpected exceptions length: got %d, want 2", len(exceptions))
	}

	first := exceptions[0].(map[string]any)
	second := exceptions[1].(map[string]any)

	if first["exception_type"] != "NullPointerException" {
		t.Fatalf("exceptions are not sorted by count descending: first=%v", first["exception_type"])
	}
	if got := first["count"].(float64); got != 12 {
		t.Fatalf("unexpected first exception count: got %v, want 12", got)
	}
	if first["deployment_environment"] != "prod" {
		t.Fatalf("unexpected deployment_environment: got %v, want prod", first["deployment_environment"])
	}

	expectedLastSeen := endTime.UTC().Format(time.RFC3339)
	if first["last_seen"] != expectedLastSeen {
		t.Fatalf("unexpected first last_seen: got %v, want %s", first["last_seen"], expectedLastSeen)
	}
	if second["last_seen"] != expectedLastSeen {
		t.Fatalf("unexpected second last_seen: got %v, want %s", second["last_seen"], expectedLastSeen)
	}
}

func TestGetExceptionsHandler_UsesDashboardShapeForEnvOnlyFilters(t *testing.T) {
	endTime := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-60 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			var reqBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode instant request body: %v", err)
			}

			query := fmt.Sprintf("%v", reqBody["query"])
			if !strings.Contains(query, "sum by (exception_type, service_name, span_name, span_kind)") {
				t.Fatalf("expected frontend grouping labels, got: %s", query)
			}
			if strings.Contains(query, "span_kind, env)") {
				t.Fatalf("query should not include env in grouping labels: %s", query)
			}
			if !strings.Contains(query, "trace_endpoint_count{env=~'alpha', exception_type!=''}[60m]") {
				t.Fatalf("expected env-only selector for endpoint count, got: %s", query)
			}
			if !strings.Contains(query, "trace_client_count{env=~'alpha', exception_type!=''}[60m]") {
				t.Fatalf("expected env-only selector for client count, got: %s", query)
			}
			if !strings.Contains(query, "trace_internal_count{env=~'alpha', exception_type!=''}[60m]") {
				t.Fatalf("expected env-only selector for internal count, got: %s", query)
			}

			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `[{"metric":{"exception_type":"IOException","service_name":"api","span_name":"GET /health","span_kind":"SPAN_KIND_SERVER"},"value":[1737374400,"5"]}]`)

		case constants.EndpointPromQuery:
			t.Fatalf("did not expect range query")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "prom-user",
		PrometheusPassword: "prom-pass",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetExceptionsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetExceptionsArgs{
		StartTimeISO:          startTime.Format(time.RFC3339),
		EndTimeISO:            endTime.Format(time.RFC3339),
		DeploymentEnvironment: "alpha",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to decode tool response: %v", err)
	}

	exceptions, ok := payload["exceptions"].([]any)
	if !ok || len(exceptions) != 1 {
		t.Fatalf("expected one exception in response, got: %v", payload["exceptions"])
	}

	first := exceptions[0].(map[string]any)
	if first["deployment_environment"] != "alpha" {
		t.Fatalf("expected deployment_environment to come from request args, got %v", first["deployment_environment"])
	}
}

func TestGetExceptionsHandler_EscapesHyphenatedServiceNamesLikeFrontend(t *testing.T) {
	endTime := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-60 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			var reqBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode instant request body: %v", err)
			}

			query := fmt.Sprintf("%v", reqBody["query"])
			if !strings.Contains(query, "service_name=~'last9\\\\-api'") {
				t.Fatalf("expected frontend-style escaping for service_name, got: %s", query)
			}
			if !strings.Contains(query, "env=~'prod'") {
				t.Fatalf("expected env filter in selector, got: %s", query)
			}

			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `[{"metric":{"exception_type":"IOException","service_name":"last9-api","span_name":"GET /health","span_kind":"SPAN_KIND_SERVER"},"value":[1737374400,"5"]}]`)

		case constants.EndpointPromQuery:
			t.Fatalf("did not expect range query")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "prom-user",
		PrometheusPassword: "prom-pass",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetExceptionsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetExceptionsArgs{
		StartTimeISO:          startTime.Format(time.RFC3339),
		EndTimeISO:            endTime.Format(time.RFC3339),
		ServiceName:           "last9-api",
		DeploymentEnvironment: "prod",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestGetExceptionsHandler_DoesNotCallRangeQuery(t *testing.T) {
	endTime := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-4 * time.Minute)

	var instantCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			instantCalls++

			var reqBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode instant request body: %v", err)
			}
			query := fmt.Sprintf("%v", reqBody["query"])

			if strings.Contains(query, "{, exception_type!=''}") {
				t.Fatalf("query includes malformed empty filter matcher: %s", query)
			}
			if strings.Contains(query, ", exception_type!=''}") {
				t.Fatalf("query includes malformed selector prefix: %s", query)
			}
			if !strings.Contains(query, "trace_endpoint_count{exception_type!=''}[4m]") {
				t.Fatalf("expected unfiltered selector with 4m window, got: %s", query)
			}

			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `[{"metric":{"exception_type":"IOException","service_name":"api","span_name":"GET /health","span_kind":"SPAN_KIND_SERVER"},"value":[1737374400,"5"]}]`)

		case constants.EndpointPromQuery:
			t.Fatalf("did not expect range query")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "prom-user",
		PrometheusPassword: "prom-pass",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetExceptionsHandler(server.Client(), cfg)
	args := GetExceptionsArgs{
		StartTimeISO: startTime.Format(time.RFC3339),
		EndTimeISO:   endTime.Format(time.RFC3339),
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if instantCalls != 1 {
		t.Fatalf("expected 1 instant call, got %d", instantCalls)
	}

	text := utils.GetTextContent(t, result)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to decode tool response: %v", err)
	}

	if got := int(payload["count"].(float64)); got != 1 {
		t.Fatalf("unexpected count: got %d, want 1", got)
	}
}

func TestGetExceptionsHandler_LastSeenUsesInstantTimestamp(t *testing.T) {
	startTime := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	endTime := startTime.Add(10 * time.Minute)
	clientLastSeen := endTime.Add(-3 * time.Minute).Unix()
	serverLastSeen := endTime.Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fmt.Sprintf(`[
				{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_CLIENT"},"value":[%d,"1"]},
				{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_SERVER"},"value":[%d,"2"]}
			]`, clientLastSeen, serverLastSeen))
		case constants.EndpointPromQuery:
			t.Fatalf("did not expect range query")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "prom-user",
		PrometheusPassword: "prom-pass",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetExceptionsHandler(server.Client(), cfg)
	args := GetExceptionsArgs{
		StartTimeISO: startTime.Format(time.RFC3339),
		EndTimeISO:   endTime.Format(time.RFC3339),
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to decode tool response: %v", err)
	}

	exceptions, ok := payload["exceptions"].([]any)
	if !ok {
		t.Fatalf("expected exceptions array in response")
	}
	if len(exceptions) != 2 {
		t.Fatalf("unexpected exceptions length: got %d, want 2", len(exceptions))
	}

	lastSeenBySpanKind := map[string]string{}
	for _, entry := range exceptions {
		exceptionMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("unexpected exception entry type: %T", entry)
		}

		spanKind, _ := exceptionMap["span_kind"].(string)
		lastSeen, _ := exceptionMap["last_seen"].(string)
		lastSeenBySpanKind[spanKind] = lastSeen
	}

	expectedServerLastSeen := time.Unix(serverLastSeen, 0).UTC().Format(time.RFC3339)
	if got := lastSeenBySpanKind["SPAN_KIND_SERVER"]; got != expectedServerLastSeen {
		t.Fatalf("unexpected server last_seen: got %q, want %q", got, expectedServerLastSeen)
	}

	expectedClientLastSeen := time.Unix(clientLastSeen, 0).UTC().Format(time.RFC3339)
	if got := lastSeenBySpanKind["SPAN_KIND_CLIENT"]; got != expectedClientLastSeen {
		t.Fatalf("unexpected client last_seen: got %q, want %q", got, expectedClientLastSeen)
	}
}
