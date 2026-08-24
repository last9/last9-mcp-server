package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
)

func TestCallAlertIntelligenceHappyPath(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != constants.EndpointAlertIntelligence {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		if got := r.Header.Get(constants.HeaderXLast9APIToken); got != constants.BearerPrefix+"test-token" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get(constants.HeaderContentType); got != constants.HeaderContentTypeJSON {
			t.Errorf("content-type = %q", got)
		}
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"rule-1","signals":[{"key":"p95_latency","eval_query":"expr","unit":"ms","query_kind":"metric"}],"group_id":"grp-1","kpi_id":"kpi-1"}`))
	}))
	defer server.Close()

	payload, err := json.Marshal(AlertIntelligenceRequest{Operation: AlertIntelCreateFromChart, ChartKey: "chart-1", SignalKey: "p95_latency"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(gotBody, []byte(`"operation":"create_from_chart"`)) {
		t.Errorf("request body missing operation: %s", gotBody)
	}
	var resp AlertIntelligenceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != "rule-1" || resp.GroupID != "grp-1" || resp.KPIID != "kpi-1" || len(resp.Signals) != 1 || resp.Signals[0].Key != "p95_latency" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAlertIntelErrorClassStrings(t *testing.T) {
	for class, want := range map[alertIntelErrorClass]string{
		classCoverageMiss:  "coverage_miss",
		classPermissions:   "permissions",
		classDuplicateName: "duplicate_name",
		classUpstream:      "upstream",
	} {
		if got := class.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestClassifyAlertIntelligenceError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       alertIntelErrorClass
	}{
		{"unknown chart key", http.StatusBadRequest, `{"message":"unknown chart key: latency_p95"}`, classCoverageMiss},
		{"unknown signal key", http.StatusBadRequest, `{"message":"unknown signal key: p95"}`, classCoverageMiss},
		{"invalid chart entity", http.StatusBadRequest, `{"message":"invalid chart entity"}`, classCoverageMiss},
		{"other 400", http.StatusBadRequest, `{"message":"invalid payload"}`, classUpstream},
		{"unauthorized", http.StatusUnauthorized, "unauthorized", classPermissions},
		{"forbidden", http.StatusForbidden, "Not allowed for viewer role", classPermissions},
		{"conflict", http.StatusConflict, "duplicate rule name", classDuplicateName},
		{"server error", http.StatusInternalServerError, "boom", classUpstream},
		{"bad gateway empty body", http.StatusBadGateway, "", classUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAlertIntelligenceError(tt.statusCode, tt.body); got != tt.want {
				t.Fatalf("class = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCallAlertIntelligenceClassifiesErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       alertIntelErrorClass
	}{
		{"coverage miss chart", http.StatusBadRequest, `{"message":"unknown chart key: latency_p95"}`, classCoverageMiss},
		{"coverage miss signal", http.StatusBadRequest, `{"message":"unknown signal key: p95"}`, classCoverageMiss},
		{"upstream 400", http.StatusBadRequest, `{"message":"invalid payload"}`, classUpstream},
		{"permissions", http.StatusForbidden, "Not allowed for viewer role", classPermissions},
		{"duplicate", http.StatusConflict, "duplicate rule name", classDuplicateName},
		{"upstream 500", http.StatusInternalServerError, "boom", classUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, tt.body, tt.statusCode)
			}))
			defer server.Close()
			_, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), []byte(`{"operation":"describe_chart"}`))
			var apiErr *AlertIntelHTTPError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected AlertIntelHTTPError, got %v", err)
			}
			if apiErr.Class != tt.want {
				t.Fatalf("class = %s, want %s", apiErr.Class, tt.want)
			}
			if apiErr.StatusCode != tt.statusCode {
				t.Fatalf("status = %d, want %d", apiErr.StatusCode, tt.statusCode)
			}
		})
	}
}

func TestCallAlertIntelligenceRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), int(maxAlertMutationResponseBytes)+1))
	}))
	defer server.Close()
	_, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), []byte(`{"operation":"describe_chart"}`))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected response cap error, got %v", err)
	}
	var apiErr *AlertIntelHTTPError
	if errors.As(err, &apiErr) {
		t.Fatalf("oversized response must not be classified/parsed as an API error: %+v", apiErr)
	}
}

func TestCallAlertIntelligenceRejectsEmptyPayload(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, payload := range [][]byte{nil, []byte("   ")} {
		_, err := callAlertIntelligence(context.Background(), server.Client(), testAlertMutationConfig(server.URL), payload)
		if err == nil || !strings.Contains(err.Error(), "payload is required") {
			t.Fatalf("expected empty payload rejection, got %v", err)
		}
	}
	if requests != 0 {
		t.Fatalf("server received %d requests for empty payloads", requests)
	}
}

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
	var req AlertIntelligenceRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	if req.Entity["serviceName"] != "checkout" {
		t.Errorf("entity serviceName = %v, want checkout", req.Entity["serviceName"])
	}
	var resp AlertIntelligenceResponse
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
	guidance := alertIntelGuidance(&AlertIntelHTTPError{StatusCode: http.StatusBadRequest, Class: classCoverageMiss, Body: "unknown chart key"})
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

func createAlertFromChartTestConfig(baseURL string) models.Config {
	cfg := testAlertMutationConfig(baseURL)
	cfg.OrgSlug = "acme"
	cfg.ClusterID = "cluster-1"
	return cfg
}

func TestCreateAlertFromChartAllDefaultsOmitted(t *testing.T) { // AE1
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"rule-9","group_id":"grp-2","kpi_id":"kpi-3"}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(result))
	}

	var raw map[string]any
	if err := json.Unmarshal(gotBody, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"threshold", "threshold_operator", "eval_window", "bad_minutes", "severity"} {
		if _, ok := raw[k]; ok {
			t.Errorf("omitted optional %q leaked into request: %s", k, gotBody)
		}
	}

	text := resultText(result)
	for _, want := range []string{
		"rule-9", "grp-2", "kpi-3", "error_ratio",
		"threshold_operator: > (backend default)",
		"threshold: 0.01 (backend default)",
		"eval_window: 5 minutes (backend default)",
		"bad_minutes: 3 (backend default)",
		"severity: breach (backend default for static chart rules)",
		"delete_alert",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q:\n%s", want, text)
		}
	}

	wantMeta := deeplink.ToMeta(deeplink.NewBuilder("acme", "cluster-1").BuildAlertingGroupsLink())
	if !reflect.DeepEqual(result.Meta, wantMeta) {
		t.Errorf("meta = %+v, want %+v", result.Meta, wantMeta)
	}
}

func TestCreateAlertFromChartExplicitScoreUnitThreshold(t *testing.T) { // AE4
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"rule-apdex","group_id":"grp-apdex"}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "apdex",
		ServiceName: "checkout",
		SignalKey:   "apdex_score",
		Name:        "checkout-apdex",
		Threshold:   float64Ptr(0.9),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(result))
	}
	if !bytes.Contains(gotBody, []byte(`"threshold":"0.9"`)) {
		t.Errorf("explicit threshold not passed through as string: %s", gotBody)
	}
	if bytes.Contains(gotBody, []byte(`"threshold":0.9`)) {
		t.Errorf("threshold serialized as number, want string: %s", gotBody)
	}
	text := resultText(result)
	if strings.Contains(text, "0.01") {
		t.Errorf("default threshold 0.01 mentioned despite explicit value:\n%s", text)
	}
	if !strings.Contains(text, "threshold: 0.9") || strings.Contains(text, "0.9 (backend default)") {
		t.Errorf("explicit threshold not reported as applied:\n%s", text)
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestCreateAlertFromChartLocalValidationRejects(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	valid := CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "apdex",
		ServiceName: "checkout",
		SignalKey:   "apdex_score",
		Name:        "checkout-apdex",
	}
	tests := []struct {
		name   string
		mutate func(*CreateAlertFromChartArgs)
	}{
		{"empty name", func(a *CreateAlertFromChartArgs) { a.Name = "   " }},
		{"negative threshold", func(a *CreateAlertFromChartArgs) { a.Threshold = float64Ptr(-0.5) }},
		{"bad_minutes exceeds eval_window", func(a *CreateAlertFromChartArgs) {
			w, b := 5, 6
			a.EvalWindow, a.BadMinutes = &w, &b
		}},
		{"bad_minutes exceeds default window", func(a *CreateAlertFromChartArgs) {
			b := 6
			a.BadMinutes = &b // default eval window is 5
		}},
		{"eval_window over 60", func(a *CreateAlertFromChartArgs) {
			w := 61
			a.EvalWindow = &w
		}},
		{"unknown operator", func(a *CreateAlertFromChartArgs) { a.ThresholdOperator = "~" }},
		{"unknown severity", func(a *CreateAlertFromChartArgs) { a.Severity = "critical" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := valid
			tt.mutate(&args)
			handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
			result, _, err := handler(context.Background(), nil, args)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("expected tool error for %s, got: %s", tt.name, resultText(result))
			}
		})
	}
	if requests != 0 {
		t.Fatalf("server received %d requests for locally invalid args", requests)
	}
}

func TestCreateAlertFromChartDuplicateNameGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "duplicate rule name", http.StatusConflict)
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"get_alert_config", "entity_id", "get_entity_alert_rules", "never retry blindly", "timeout", "duplicate"} {
		if !strings.Contains(text, want) {
			t.Errorf("duplicate-name guidance missing %q:\n%s", want, text)
		}
	}
}

func TestAlertIntelEntityAttrsCannotOverrideReservedKeys(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	if _, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
		Env:         "prod",
		Attributes:  map[string]string{"serviceName": "evil", "env": "staging", "region": "us-east-1"},
	}); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Entity map[string]string `json:"entity"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	if req.Entity["serviceName"] != "checkout" {
		t.Errorf("serviceName overridden via attributes: %v", req.Entity)
	}
	if req.Entity["env"] != "prod" {
		t.Errorf("env overridden via attributes: %v", req.Entity)
	}
	if req.Entity["region"] != "us-east-1" {
		t.Errorf("non-reserved attribute dropped: %v", req.Entity)
	}
}

func TestCreateAlertFromChartTransportFailureIsToolError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	client := server.Client()
	server.Close() // nothing is listening; the request fails at the transport level

	handler := NewCreateAlertFromChartHandler(client, createAlertFromChartTestConfig(baseURL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatalf("transport failure must surface as tool error result, got Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"may or may not have been applied", "get_alert_config"} {
		if !strings.Contains(text, want) {
			t.Errorf("transport-failure guidance missing %q:\n%s", want, text)
		}
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
	if !strings.Contains(err.Error(), "alert intelligence request failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateAlertFromChartGarbageBodyIsParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json{{`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to parse") {
		t.Fatalf("expected parse failure error, got result %v err %v", result, err)
	}
}

func TestCreateAlertFromChartSuccessWithoutRuleID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatalf("no-rule-id must surface as tool error result, got Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"no rule id", "get_alert_config"} {
		if !strings.Contains(text, want) {
			t.Errorf("no-rule-id guidance missing %q:\n%s", want, text)
		}
	}
}

func TestCreateAlertFromChartPermissionsGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "viewer role not allowed", http.StatusForbidden)
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got: %s", resultText(result))
	}
	text := resultText(result)
	if !strings.Contains(text, "credentials") || !strings.Contains(text, "role") {
		t.Errorf("permission guidance missing credential framing:\n%s", text)
	}
	if strings.Contains(text, "Discover page") {
		t.Errorf("permission guidance must be distinct from coverage text:\n%s", text)
	}
}
