package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"
)

func TestDescribeAlertChartHappyPath(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"signals":[{"key":"p95_latency","unit":"ms","query_kind":"metric"}],"pointers":{"view_source":"/x","view_in":"/y"}}`))
	}))
	defer server.Close()

	handler := NewDescribeAlertChartHandler(server.Client(), testAlertMutationConfig(server.URL))
	result, _, err := handler(context.Background(), nil, DescribeAlertChartArgs{
		Surface:     "discover-service",
		ChartKey:    "response_time",
		ServiceName: "checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(result))
	}
	if !bytes.Contains(gotBody, []byte(`"operation":"describe_chart"`)) {
		t.Errorf("request body missing operation describe_chart: %s", gotBody)
	}
	var req alerts.IntelligenceRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	if req.Entity["serviceName"] != "checkout" {
		t.Errorf("entity serviceName = %v, want checkout", req.Entity["serviceName"])
	}
	var resp alerts.IntelligenceResponse
	if err := json.Unmarshal([]byte(resultText(result)), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if len(resp.Signals) != 1 || resp.Signals[0].Key != "p95_latency" || resp.Signals[0].Unit != "ms" {
		t.Fatalf("signals changed in transit: %+v", resp)
	}
}

func TestDescribeAlertChartCoverageMiss(t *testing.T) {
	reason := `{"message":"unknown chart key: latency_p95"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, reason, http.StatusBadRequest)
	}))
	defer server.Close()

	handler := NewDescribeAlertChartHandler(server.Client(), testAlertMutationConfig(server.URL))
	result, _, err := handler(context.Background(), nil, DescribeAlertChartArgs{
		Surface:     "discover-service",
		ChartKey:    "latency_p95",
		ServiceName: "checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error result, got: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"unknown chart key: latency_p95", "Discover page", "dashboard or API", "Then pick a signal_key from that output when creating"} {
		if !strings.Contains(text, want) {
			t.Errorf("guidance missing %q: %s", want, text)
		}
	}
}

func TestCoverageMissCoordinatesAreDescribeArgsTags(t *testing.T) {
	guidance := alertIntelGuidance(&utils.UpstreamHTTPError{StatusCode: http.StatusBadRequest, Body: "unknown chart key"})
	marker := "coordinates ("
	start := strings.Index(guidance, marker)
	if start < 0 {
		t.Fatalf("guidance missing coordinate parenthetical: %s", guidance)
	}
	start += len(marker)
	end := strings.Index(guidance[start:], ")")
	if end < 0 {
		t.Fatalf("unterminated coordinate parenthetical: %s", guidance)
	}
	tokens := strings.Split(guidance[start:start+end], ", ")
	if len(tokens) == 0 {
		t.Fatal("no coordinate tokens found")
	}

	tags := map[string]bool{}
	typ := reflect.TypeOf(DescribeAlertChartArgs{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			tags[tag] = true
		}
	}
	for _, tok := range tokens {
		if !tags[tok] {
			t.Errorf("coverage-miss coordinate %q is not a DescribeAlertChartArgs json tag", tok)
		}
	}
}

func TestDescribeAlertChartMissingRequiredFields(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler := NewDescribeAlertChartHandler(server.Client(), testAlertMutationConfig(server.URL))
	for _, args := range []DescribeAlertChartArgs{
		{Surface: "", ChartKey: "apdex", ServiceName: "checkout"},
		{Surface: "discover-service", ChartKey: "apdex", ServiceName: ""},
	} {
		result, _, err := handler(context.Background(), nil, args)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("expected tool error result for %+v", args)
		}
	}
	if requests != 0 {
		t.Fatalf("server received %d requests for invalid args", requests)
	}
}

func TestDescribeAlertChartRejectsUncoveredSurface(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
	}))
	defer server.Close()
	result, _, err := NewDescribeAlertChartHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
		DescribeAlertChartArgs{Surface: "dashboards", ChartKey: "apdex", ServiceName: "checkout"})
	if err != nil || !result.IsError || requests != 0 {
		t.Fatalf("result=%s err=%v requests=%d", resultText(result), err, requests)
	}
	for _, want := range []string{"discover-service", "discover-exceptions", "create_alert"} {
		if !strings.Contains(resultText(result), want) {
			t.Errorf("message missing %q: %s", want, resultText(result))
		}
	}
}

func TestDescribeAlertChartUncoveredSurfaceReadOnlyNamesNoHiddenTool(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused.invalid")
	cfg.ToolFilter = toolsets.Filter{ReadOnly: true}
	result, _, err := NewDescribeAlertChartHandler(http.DefaultClient, cfg)(context.Background(), nil,
		DescribeAlertChartArgs{Surface: "dashboards", ChartKey: "apdex", ServiceName: "checkout"})
	if err != nil || !result.IsError {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
	text := resultText(result)
	if strings.Contains(text, "create_alert") || !strings.Contains(text, "KPI of the alert group") {
		t.Fatalf("read-only message should point at a KPI rule without naming create_alert: %s", text)
	}
}

func TestDescribeAlertChartAttributesPassThroughVerbatim(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	handler := NewDescribeAlertChartHandler(server.Client(), testAlertMutationConfig(server.URL))
	if _, _, err := handler(context.Background(), nil, DescribeAlertChartArgs{
		Surface:     "discover-exceptions",
		ChartKey:    "exception_count",
		ServiceName: "payments",
		Env:         "prod",
		Attributes:  map[string]string{"exception_type": "TimeoutError", "operation": "POST /charge"},
	}); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Entity map[string]string `json:"entity"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"serviceName":    "payments",
		"env":            "prod",
		"exception_type": "TimeoutError",
		"operation":      "POST /charge",
	}
	for k, v := range want {
		if req.Entity[k] != v {
			t.Errorf("entity[%q] = %q, want %q", k, req.Entity[k], v)
		}
	}
	if len(req.Entity) != len(want) {
		t.Errorf("entity has unexpected keys: %v", req.Entity)
	}
}

func TestDescribeAlertChartTransportFailureStaysRawError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	client := server.Client()
	server.Close()

	handler := NewDescribeAlertChartHandler(client, testAlertMutationConfig(baseURL))
	result, _, err := handler(context.Background(), nil, DescribeAlertChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
	})
	if err == nil {
		t.Fatal("describe transport failure must keep the raw error path")
	}
	if result != nil {
		t.Errorf("describe transport failure must not return a result: %s", resultText(result))
	}
	if !strings.Contains(err.Error(), "alert intelligence") {
		t.Errorf("unexpected error: %v", err)
	}
}
