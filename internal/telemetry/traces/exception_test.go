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

	var instantCalls, rangeCalls int

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
			if !strings.Contains(query, "span_name=~'POST \\/orders'") {
				t.Fatalf("span_name filter missing from instant query: %s", query)
			}
			if !strings.Contains(query, "env=~'prod'") {
				t.Fatalf("deployment_environment filter was not translated to env: %s", query)
			}
			if !strings.Contains(query, "exception_type!=''") {
				t.Fatalf("exception_type guard missing from instant query: %s", query)
			}
			if !strings.Contains(query, "sum by (exception_type, service_name, span_name, span_kind, env)") {
				t.Fatalf("instant query is missing env in grouping labels: %s", query)
			}
			if !strings.Contains(query, "[30m]") {
				t.Fatalf("expected 30m range selector in instant query: %s", query)
			}

			if got := int64(reqBody["timestamp"].(float64)); got != endTime.Unix() {
				t.Fatalf("unexpected instant timestamp: got %d, want %d", got, endTime.Unix())
			}

			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `[
				{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_CLIENT"},"value":[1737369000,"3"]},
				{"metric":{"exception_type":"NullPointerException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_SERVER"},"value":[1737369000,"12"]}
			]`)

		case constants.EndpointPromQuery:
			rangeCalls++

			var reqBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode range request body: %v", err)
			}

			query := fmt.Sprintf("%v", reqBody["query"])
			if !strings.Contains(query, "[1m]") {
				t.Fatalf("expected 1m range selector in last-seen query: %s", query)
			}
			if got := int64(reqBody["timestamp"].(float64)); got != endTime.Unix() {
				t.Fatalf("unexpected range timestamp: got %d, want %d", got, endTime.Unix())
			}
			if got := int64(reqBody["window"].(float64)); got != 300 {
				t.Fatalf("unexpected range window: got %d, want 300", got)
			}
			if got := int(reqBody["step"].(float64)); got != 60 {
				t.Fatalf("unexpected range step: got %d, want 60", got)
			}

			lastSeenNullPointer := endTime.Add(-2 * time.Minute).Unix()
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fmt.Sprintf(`[
				{"metric":{"exception_type":"NullPointerException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_SERVER"},"values":[[%d,"0"],[%d,"2"],[%d,"0"]]},
				{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_CLIENT"},"values":[[%d,"0"],[%d,"0"],[%d,"0"]]}
			]`,
				endTime.Add(-4*time.Minute).Unix(),
				lastSeenNullPointer,
				endTime.Unix(),
				endTime.Add(-4*time.Minute).Unix(),
				endTime.Add(-2*time.Minute).Unix(),
				endTime.Unix(),
			))

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
	if rangeCalls != 1 {
		t.Fatalf("expected 1 range query call for last-seen, got %d", rangeCalls)
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

	expectedLastSeenFirst := endTime.Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	if first["last_seen"] != expectedLastSeenFirst {
		t.Fatalf("unexpected first last_seen: got %v, want %s", first["last_seen"], expectedLastSeenFirst)
	}

	expectedLastSeenSecond := endTime.UTC().Format(time.RFC3339)
	if second["last_seen"] != expectedLastSeenSecond {
		t.Fatalf("unexpected fallback last_seen: got %v, want %s", second["last_seen"], expectedLastSeenSecond)
	}
}

func TestGetExceptionsHandler_SkipsLastSeenRangeForShortWindow(t *testing.T) {
	endTime := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-4 * time.Minute)

	var instantCalls, rangeCalls int

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
			if !strings.Contains(query, "trace_endpoint_count{exception_type!=''}[4m]") {
				t.Fatalf("expected unfiltered selector with 4m window, got: %s", query)
			}

			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `[{"metric":{"exception_type":"IOException","service_name":"api","span_name":"GET /health","span_kind":"SPAN_KIND_SERVER"},"value":[1737374400,"5"]}]`)

		case constants.EndpointPromQuery:
			rangeCalls++
			t.Fatalf("did not expect range query for short window")
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
	if rangeCalls != 0 {
		t.Fatalf("expected no range calls, got %d", rangeCalls)
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

func TestGetExceptionsHandler_LastSeenKeyIncludesSpanKind(t *testing.T) {
	startTime := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	endTime := startTime.Add(10 * time.Minute)
	oldLastSeen := endTime.Add(-3 * time.Minute).Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `[{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_CLIENT"},"value":[1737369000,"1"]},{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_SERVER"},"value":[1737369000,"2"]}]`)
		case constants.EndpointPromQuery:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fmt.Sprintf(`[{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_SERVER"},"values":[[%d,"0"],[%d,"0"],[%d,"4"]]},{"metric":{"exception_type":"TimeoutException","service_name":"checkout","span_name":"POST /orders","span_kind":"SPAN_KIND_CLIENT"},"values":[[%d,"0"],[%d,"3"],[%d,"0"]]}]`,
				endTime.Add(-5*time.Minute).Unix(),
				endTime.Add(-2*time.Minute).Unix(),
				endTime.Unix(),
				endTime.Add(-5*time.Minute).Unix(),
				oldLastSeen,
				endTime.Unix(),
			))
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

	expectedServerLastSeen := endTime.UTC().Format(time.RFC3339)
	if got := lastSeenBySpanKind["SPAN_KIND_SERVER"]; got != expectedServerLastSeen {
		t.Fatalf("unexpected server last_seen: got %q, want %q", got, expectedServerLastSeen)
	}

	expectedClientLastSeen := time.Unix(oldLastSeen, 0).UTC().Format(time.RFC3339)
	if got := lastSeenBySpanKind["SPAN_KIND_CLIENT"]; got != expectedClientLastSeen {
		t.Fatalf("unexpected client last_seen: got %q, want %q", got, expectedClientLastSeen)
	}
}
