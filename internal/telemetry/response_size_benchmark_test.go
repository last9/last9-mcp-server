package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestResponseSizeBenchmark measures the total response size (in bytes) of key MCP tool handlers
// when given realistic verbose mock data. This is the primary metric for the autoresearch loop.
func TestResponseSizeBenchmark(t *testing.T) {
	total := 0

	// --- get_traces: 50 spans with verbose attributes ---
	traceBytes := measureGetTracesResponseSize(t, 50)
	fmt.Printf("METRIC get_traces_bytes=%d\n", traceBytes)
	total += traceBytes

	// --- get_logs: 50 log streams with verbose entries ---
	logBytes := measureGetLogsResponseSize(t, 50)
	fmt.Printf("METRIC get_logs_bytes=%d\n", logBytes)
	total += logBytes

	// --- get_service_logs: 20 log entries ---
	serviceLogBytes := measureGetServiceLogsResponseSize(t, 20)
	fmt.Printf("METRIC get_service_logs_bytes=%d\n", serviceLogBytes)
	total += serviceLogBytes

	// --- prometheus_range_query: 10 series x 60 points ---
	promRangeBytes := measurePromRangeQueryResponseSize(t, 10, 60)
	fmt.Printf("METRIC prom_range_query_bytes=%d\n", promRangeBytes)
	total += promRangeBytes

	fmt.Printf("METRIC total_response_bytes=%d\n", total)
}

func testConfig(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

func measureGetTracesResponseSize(t *testing.T, numTraces int) int {
	t.Helper()
	mockResp := buildVerboseTraceResponse(numTraces)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResp))
	}))
	defer server.Close()

	handler := traces.NewGetTracesHandler(server.Client(), testConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, traces.GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: now.Add(-5 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Limit:        numTraces,
	})
	if err != nil {
		t.Fatalf("get_traces handler error: %v", err)
	}

	return len(result.Content[0].(*mcp.TextContent).Text)
}

func measureGetLogsResponseSize(t *testing.T, numStreams int) int {
	t.Helper()
	mockResp := buildVerboseLogResponse(numStreams)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResp))
	}))
	defer server.Close()

	handler := logs.NewGetLogsHandler(server.Client(), testConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, logs.GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"Body"}}},
		},
		StartTimeISO: now.Add(-5 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Limit:        numStreams * 5,
	})
	if err != nil {
		t.Fatalf("get_logs handler error: %v", err)
	}

	return len(result.Content[0].(*mcp.TextContent).Text)
}

func measureGetServiceLogsResponseSize(t *testing.T, numEntries int) int {
	t.Helper()
	mockResp := buildVerboseServiceLogResponse(numEntries)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResp))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	handler := logs.NewGetServiceLogsHandler(server.Client(), cfg)
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, logs.GetServiceLogsArgs{
		Service:      "test-service",
		StartTimeISO: now.Add(-5 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Limit:        numEntries,
		Index:        "physical_index:test",
	})
	if err != nil {
		t.Fatalf("get_service_logs handler error: %v", err)
	}

	return len(result.Content[0].(*mcp.TextContent).Text)
}

func measurePromRangeQueryResponseSize(t *testing.T, numSeries, pointsPerSeries int) int {
	t.Helper()
	mockResp := buildVerbosePromRangeResponse(numSeries, pointsPerSeries)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResp))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	handler := apm.NewPromqlRangeQueryHandler(server.Client(), cfg)
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, apm.PromqlRangeQueryArgs{
		Query:        "rate(http_requests_total[5m])",
		StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("prometheus_range_query handler error: %v", err)
	}

	return len(result.Content[0].(*mcp.TextContent).Text)
}

// buildVerbosePromRangeResponse creates a mock Prometheus range query response
// with multiple series and data points.
func buildVerbosePromRangeResponse(numSeries, pointsPerSeries int) string {
	services := []string{"api-gateway", "auth-service", "user-service", "payment-service", "order-service",
		"notification-service", "search-service", "catalog-service", "inventory-service", "shipping-service"}
	methods := []string{"GET", "POST", "PUT", "DELETE"}

	series := make([]map[string]interface{}, 0, numSeries)
	for i := 0; i < numSeries; i++ {
		values := make([][]interface{}, 0, pointsPerSeries)
		for j := 0; j < pointsPerSeries; j++ {
			ts := 1700000000 + j*60
			val := fmt.Sprintf("%.3f", float64(100+i*10+j%20))
			values = append(values, []interface{}{float64(ts), val})
		}
		series = append(series, map[string]interface{}{
			"metric": map[string]interface{}{
				"__name__":                  "http_requests_total",
				"service":                   services[i%len(services)],
				"method":                    methods[i%len(methods)],
				"status":                    "200",
				"instance":                  fmt.Sprintf("10.0.%d.%d:8080", i/10, i%10),
				"job":                       "kubernetes-pods",
				"namespace":                 "production",
				"pod":                       fmt.Sprintf("svc-%d-7b9f4d6c8-x%04d", i, i),
				"deployment_environment":    "production",
			},
			"values": values,
		})
	}

	body, _ := json.Marshal(map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result":     series,
		},
	})
	return string(body)
}

// buildVerboseTraceResponse creates a mock API response with verbose trace spans
// (ResourceAttributes, SpanAttributes, Events, Links, etc.) to simulate real-world bloat.
func buildVerboseTraceResponse(n int) string {
	items := make([]map[string]interface{}, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, map[string]interface{}{
			"TraceId":     fmt.Sprintf("abcdef1234567890abcdef12345678%02d", i),
			"SpanId":      fmt.Sprintf("1234567890abcd%02d", i),
			"ParentSpanId": fmt.Sprintf("parent-span-%d", i),
			"SpanKind":    "SPAN_KIND_SERVER",
			"SpanName":    fmt.Sprintf("GET /api/v1/users/%d", i),
			"ServiceName": "api-gateway",
			"Duration":    150000 + i*1000,
			"Timestamp":   fmt.Sprintf("2025-11-02T10:%02d:00Z", i%60),
			"StatusCode":  "STATUS_CODE_OK",
			"TraceState":  "",
			"ResourceAttributes": map[string]interface{}{
				"service.name":              "api-gateway",
				"service.namespace":         "production",
				"service.version":           "v2.14.3",
				"deployment.environment":    "production",
				"k8s.namespace.name":        "default",
				"k8s.pod.name":              fmt.Sprintf("api-gateway-7b9f4d6c8-x%04d", i),
				"k8s.node.name":             fmt.Sprintf("ip-10-0-%d-%d.ec2.internal", i/10, i%10),
				"k8s.container.name":        "api-gateway",
				"k8s.deployment.name":       "api-gateway",
				"k8s.replicaset.name":       "api-gateway-7b9f4d6c8",
				"host.name":                 fmt.Sprintf("ip-10-0-%d-%d", i/10, i%10),
				"host.arch":                 "amd64",
				"os.type":                   "linux",
				"os.description":            "Ubuntu 22.04.3 LTS",
				"process.pid":               12345 + i,
				"process.command":           "/usr/local/bin/api-gateway",
				"process.runtime.name":      "go",
				"process.runtime.version":   "1.21.3",
				"telemetry.sdk.name":        "opentelemetry",
				"telemetry.sdk.language":    "go",
				"telemetry.sdk.version":     "1.20.0",
				"cloud.provider":            "aws",
				"cloud.region":              "ap-south-1",
				"cloud.availability_zone":   "ap-south-1a",
			},
			"SpanAttributes": map[string]interface{}{
				"http.method":                "GET",
				"http.url":                   fmt.Sprintf("https://api.example.com/v1/users/%d", i),
				"http.target":               fmt.Sprintf("/v1/users/%d", i),
				"http.host":                 "api.example.com",
				"http.scheme":               "https",
				"http.status_code":          200,
				"http.user_agent":           "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
				"http.request_content_length": 0,
				"http.response_content_length": 1234 + i,
				"net.peer.ip":               fmt.Sprintf("10.0.%d.%d", i/10, i%10),
				"net.peer.port":             8080,
				"net.host.name":             "api.example.com",
				"net.host.port":             443,
				"rpc.system":                "grpc",
				"rpc.service":               "UserService",
				"rpc.method":                "GetUser",
				"db.system":                 "postgresql",
				"db.statement":              fmt.Sprintf("SELECT * FROM users WHERE id = %d", i),
			},
			"Events": []interface{}{},
			"Links":  []interface{}{},
		})
	}
	body, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{"result": items},
	})
	return string(body)
}

// buildVerboseLogResponse creates a mock logs API response in Loki streams format.
func buildVerboseLogResponse(numStreams int) string {
	streams := make([]map[string]interface{}, 0, numStreams)
	for i := 0; i < numStreams; i++ {
		values := make([][]interface{}, 0, 5)
		for j := 0; j < 5; j++ {
			ts := fmt.Sprintf("%d000000000", 1700000000+i*60+j)
			// Alternate between short messages and long stack traces (realistic mix)
			var msg string
			if j%2 == 0 {
				// Long message with stack trace (~2000 chars) — common in real error logs
				msg = fmt.Sprintf(`{"level":"error","msg":"connection timeout to upstream service after 30s retry","service":"api-gateway","trace_id":"abcdef%06d","span_id":"123456%04d","request_id":"req-%d-%d","method":"GET","path":"/api/v1/users/%d","status":503,"duration_ms":30012,"error":"context deadline exceeded","stack":"goroutine 1234 [running]:\nruntime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:24\nmain.(*Server).handleRequest(0xc0001234%02d, {0x7f8b2c000000, 0xc00012%04d})\n\t/app/internal/server/handler.go:123 +0x1a4\nmain.(*Server).ServeHTTP(0xc000456000, {0x7f8b2c000000, 0xc00012%04d}, 0xc0001234%02d)\n\t/app/internal/server/server.go:89 +0x2b8\nnet/http.serverHandler.ServeHTTP({0xc000789000}, {0x7f8b2c000000, 0xc00012%04d}, 0xc0001234%02d)\n\t/usr/local/go/src/net/http/server.go:2936 +0x316\nnet/http.(*conn).serve(0xc000abc000, {0x7f8b2c000100, 0xc000def000})\n\t/usr/local/go/src/net/http/server.go:1995 +0x612\ncreated by net/http.(*Server).Serve\n\t/usr/local/go/src/net/http/server.go:3089 +0x5ed","upstream_response":{"status":504,"body":"gateway timeout after 30000ms","headers":{"x-request-id":"req-%d-%d","x-trace-id":"abcdef%06d"}}}`, i, j, i, j, i, j, j, j, j, j, j, i, j, i)
			} else {
				// Short message (~200 chars)
				msg = fmt.Sprintf(`{"level":"info","msg":"request completed successfully","service":"api-gateway","trace_id":"abcdef%06d","method":"GET","path":"/api/v1/users/%d","status":200,"duration_ms":45}`, i, i)
			}
			values = append(values, []interface{}{ts, msg})
		}
		streams = append(streams, map[string]interface{}{
			"stream": map[string]interface{}{
				"service_name":            fmt.Sprintf("service-%d", i%10),
				"severity":                "error",
				"deployment_environment":  "production",
				"k8s_namespace_name":      "default",
				"k8s_pod_name":            fmt.Sprintf("svc-%d-pod-abc123", i),
				"k8s_container_name":      fmt.Sprintf("svc-%d", i%10),
			},
			"values": values,
		})
	}
	body, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "streams",
			"result":     streams,
		},
	})
	return string(body)
}

// buildVerboseServiceLogResponse creates a mock service logs API response.
func buildVerboseServiceLogResponse(numEntries int) string {
	streams := make([]map[string]interface{}, 0, 1)
	values := make([][]interface{}, 0, numEntries)
	for i := 0; i < numEntries; i++ {
		ts := fmt.Sprintf("%d000000000", 1700000000+i*60)
		msg := fmt.Sprintf(`{"level":"error","msg":"request failed with status 503: upstream connection timeout after 30000ms","trace_id":"abc%06d","span_id":"def%04d","request_id":"req-%d","method":"POST","path":"/api/v1/orders","error":"context deadline exceeded","retry_count":3,"duration_ms":30015}`, i, i, i)
		values = append(values, []interface{}{ts, msg})
	}
	streams = append(streams, map[string]interface{}{
		"stream": map[string]interface{}{
			"service_name": "test-service",
			"severity":     "error",
		},
		"values": values,
	})
	body, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "streams",
			"result":     streams,
		},
	})
	return string(body)
}
