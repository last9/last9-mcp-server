package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testDropRuleConfig(actionURL string) models.Config {
	return models.Config{
		APIBaseURL: actionURL,
		ActionURL:  actionURL,
		Region:     "us-east-1",
		OrgSlug:    "test-org",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestValidateAddDropRuleArgs(t *testing.T) {
	if err := validateAddDropRuleArgs(AddDropRuleArgs{
		Filters: []DropRuleFilter{{Key: `attributes["level"]`, Value: "debug"}},
	}); err == nil {
		t.Fatal("expected missing name error")
	}

	if err := validateAddDropRuleArgs(AddDropRuleArgs{
		Name: "rule",
	}); err == nil {
		t.Fatal("expected missing filters error")
	}

	if err := validateAddDropRuleArgs(AddDropRuleArgs{
		Name:    "rule",
		Filters: []DropRuleFilter{{Key: `attributes["level"]`, Value: "debug"}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertDropRuleFilters(t *testing.T) {
	filters, err := convertDropRuleFilters([]DropRuleFilter{
		{Key: `attributes["level"]`, Value: "debug"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filters) != 1 {
		t.Fatalf("expected one filter, got %d", len(filters))
	}
	if filters[0].Operator != "equals" {
		t.Fatalf("expected default operator equals, got %q", filters[0].Operator)
	}
	if filters[0].Conjunction != "and" {
		t.Fatalf("expected default conjunction and, got %q", filters[0].Conjunction)
	}

	// Key format is the API's contract; only non-empty is checked here.
	if _, err := convertDropRuleFilters([]DropRuleFilter{
		{Key: "", Value: "checkout"},
	}); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestReadDropRuleAPIResponseEmptyBodyOnSuccess(t *testing.T) {
	// Each handler keeps the JSON shape its populated responses have: a list for
	// get_drop_rules, an object for add_drop_rule.
	call := map[string]func(*testing.T, *httptest.Server) *mcp.CallToolResult{
		"[]": func(t *testing.T, server *httptest.Server) *mcp.CallToolResult {
			handler := NewGetDropRulesHandler(server.Client(), testDropRuleConfig(server.URL))
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDropRulesArgs{})
			if err != nil {
				t.Fatalf("get_drop_rules: expected empty 2xx body to be a success, got %v", err)
			}
			return result
		},
		"{}": func(t *testing.T, server *httptest.Server) *mcp.CallToolResult {
			handler := NewAddDropRuleHandler(server.Client(), testDropRuleConfig(server.URL))
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
				Name:    "drop-test-service",
				Filters: []DropRuleFilter{{Key: `attributes["level"]`, Value: "debug"}},
			})
			if err != nil {
				t.Fatalf("add_drop_rule: expected empty 2xx body to be a success, got %v", err)
			}
			return result
		},
	}

	for want, invoke := range call {
		for _, status := range []int{http.StatusCreated, http.StatusNoContent, http.StatusOK} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			result := invoke(t, server)
			server.Close()

			text, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("status %d: expected text content, got %T", status, result.Content[0])
			}
			if text.Text != want {
				t.Fatalf("status %d: expected %s, got %q", status, want, text.Text)
			}
		}
	}
}

func TestAddDropRuleHandlerContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := NewAddDropRuleHandler(server.Client(), testDropRuleConfig(server.URL))
	_, _, err := handler(ctx, &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name: "test-rule",
		Filters: []DropRuleFilter{
			{Key: `attributes["service_name"]`, Value: "test-service", Operator: "equals", Conjunction: "and"},
		},
	})

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestAddDropRuleHandlerSendsCorrectPayload(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedQuery url.Values
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "rule-123"})
	}))
	defer server.Close()

	cfg := testDropRuleConfig(server.URL)
	handler := NewAddDropRuleHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name: "drop-test-service",
		Filters: []DropRuleFilter{
			{Key: `attributes["service_name"]`, Value: "test-service", Operator: "equals", Conjunction: "and"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if capturedMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/otel_settings/drop" {
		t.Fatalf("expected path /otel_settings/drop, got %s", capturedPath)
	}
	if capturedQuery.Get("region") != cfg.Region {
		t.Fatalf("expected region=%s, got %s", cfg.Region, capturedQuery.Get("region"))
	}
	if capturedQuery.Get("cluster_id") != cfg.ClusterID {
		t.Fatalf("expected cluster_id=%s, got %s", cfg.ClusterID, capturedQuery.Get("cluster_id"))
	}
	if capturedBody["name"] != "drop-test-service" {
		t.Fatalf("expected name=drop-test-service, got %v", capturedBody["name"])
	}

	properties, ok := capturedBody["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", capturedBody["properties"])
	}
	if properties["telemetry"] != TELEMETRY_LOGS {
		t.Fatalf("expected telemetry=%s, got %v", TELEMETRY_LOGS, properties["telemetry"])
	}

	filters, ok := properties["filters"].([]interface{})
	if !ok || len(filters) != 1 {
		t.Fatalf("expected one filter, got %v", properties["filters"])
	}
	filter, ok := filters[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected filter object, got %T", filters[0])
	}
	if filter["key"] != `attributes["service_name"]` {
		t.Fatalf("unexpected filter key: %v", filter["key"])
	}
	if filter["operator"] != "equals" {
		t.Fatalf("unexpected filter operator: %v", filter["operator"])
	}

	action, ok := properties["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected action object, got %T", properties["action"])
	}
	if action["name"] != DROP_RULE_ACTION_NAME {
		t.Fatalf("expected action name=%s, got %v", DROP_RULE_ACTION_NAME, action["name"])
	}
	if action["destination"] != "" {
		t.Fatalf("expected empty destination, got %v", action["destination"])
	}
}

func TestAddDropRuleHandlerAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	handler := NewAddDropRuleHandler(server.Client(), testDropRuleConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name: "drop-test-service",
		Filters: []DropRuleFilter{
			{Key: `attributes["level"]`, Value: "debug"},
		},
	})
	if err == nil {
		t.Fatal("expected API error, got nil")
	}
	if !strings.Contains(err.Error(), "add_drop_rule:") {
		t.Fatalf("expected wrapped add_drop_rule error, got %v", err)
	}
	if !strings.Contains(err.Error(), "405") {
		t.Fatalf("expected status in error, got %v", err)
	}
	// 405 is neither 400 nor 422, so the upstream body is drained and omitted
	// from the tool error — only the status + advice are relayed.
	if strings.Contains(err.Error(), "method not allowed") {
		t.Fatalf("non-400/422 body leaked into error: %v", err)
	}
}

func TestAddDropRuleHandlerValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for invalid input")
	}))
	defer server.Close()

	handler := NewAddDropRuleHandler(server.Client(), testDropRuleConfig(server.URL))

	tests := []struct {
		name string
		args AddDropRuleArgs
	}{
		{
			name: "missing rule name",
			args: AddDropRuleArgs{
				Filters: []DropRuleFilter{{Key: `attributes["service_name"]`, Value: "svc"}},
			},
		},
		{
			name: "empty filters",
			args: AddDropRuleArgs{
				Name:    "my-rule",
				Filters: []DropRuleFilter{},
			},
		},
		{
			name: "invalid operator",
			args: AddDropRuleArgs{
				Name: "my-rule",
				Filters: []DropRuleFilter{
					{Key: `attributes["service_name"]`, Value: "svc", Operator: "contains"},
				},
			},
		},
		{
			name: "invalid conjunction",
			args: AddDropRuleArgs{
				Name: "my-rule",
				Filters: []DropRuleFilter{
					{Key: `attributes["service_name"]`, Value: "svc", Conjunction: "or"},
				},
			},
		},
		{
			name: "missing filter key",
			args: AddDropRuleArgs{
				Name:    "my-rule",
				Filters: []DropRuleFilter{{Value: "svc"}},
			},
		},
		{
			name: "missing filter value",
			args: AddDropRuleArgs{
				Name:    "my-rule",
				Filters: []DropRuleFilter{{Key: `attributes["service_name"]`}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), "add_drop_rule:") {
				t.Fatalf("expected wrapped add_drop_rule error, got %v", err)
			}
		})
	}
}

func TestAddDropRuleHandlerRequiresRegionAndCluster(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when config is incomplete")
	}))
	defer server.Close()

	cfg := testDropRuleConfig(server.URL)
	cfg.Region = ""
	handler := NewAddDropRuleHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name: "my-rule",
		Filters: []DropRuleFilter{
			{Key: `attributes["level"]`, Value: "debug"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing region, got nil")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Fatalf("expected region error, got %v", err)
	}

	cfg = testDropRuleConfig(server.URL)
	cfg.ClusterID = ""
	handler = NewAddDropRuleHandler(server.Client(), cfg)
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name: "my-rule",
		Filters: []DropRuleFilter{
			{Key: `attributes["level"]`, Value: "debug"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing cluster_id, got nil")
	}
	if !strings.Contains(err.Error(), "cluster_id") {
		t.Fatalf("expected cluster_id error, got %v", err)
	}
}

func TestGetDropRulesHandlerUsesOTelEndpoint(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedQuery url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{{"id": "rule-1"}})
	}))
	defer server.Close()

	cfg := testDropRuleConfig(server.URL)
	handler := NewGetDropRulesHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDropRulesArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if capturedMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", capturedMethod)
	}
	if capturedPath != "/otel_settings/drop" {
		t.Fatalf("expected path /otel_settings/drop, got %s", capturedPath)
	}
	if capturedQuery.Get("region") != cfg.Region {
		t.Fatalf("expected region=%s, got %s", cfg.Region, capturedQuery.Get("region"))
	}
	if capturedQuery.Get("cluster_id") != "" {
		t.Fatalf("did not expect cluster_id on list request, got %s", capturedQuery.Get("cluster_id"))
	}
}

func TestGetDropRulesHandlerAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	handler := NewGetDropRulesHandler(server.Client(), testDropRuleConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDropRulesArgs{})
	if err == nil {
		t.Fatal("expected API error, got nil")
	}
	if !strings.Contains(err.Error(), "get_drop_rules:") {
		t.Fatalf("expected wrapped get_drop_rules error, got %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected status in error, got %v", err)
	}
}

func TestGetDropRulesHandlerContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := NewGetDropRulesHandler(server.Client(), testDropRuleConfig(server.URL))
	_, _, err := handler(ctx, &mcp.CallToolRequest{}, GetDropRulesArgs{})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestDropRuleHandlerUpstreamErrorRedactsAndDrains(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
		forbid     []string
	}{
		{
			name:       "400 redacts URLs and credentials",
			status:     http.StatusBadRequest,
			body:       `{"error":"invalid rule","url":"https://internal.example/otel_settings/drop?token=SECRETVALUE","authorization":"Bearer s3cr3tT0k3n","api_key":"ak_live_xyz"}`,
			wantSubstr: "[redacted-url]",
			forbid:     []string{"SECRETVALUE", "s3cr3tT0k3n", "ak_live_xyz", "https://internal.example"},
		},
		{
			name:       "422 redacts URLs and credentials",
			status:     http.StatusUnprocessableEntity,
			body:       `{"error":"invalid","url":"https://internal.example/x?token=SECRETVALUE","api_key":"ak_live_xyz"}`,
			wantSubstr: "[redacted-url]",
			forbid:     []string{"SECRETVALUE", "ak_live_xyz", "https://internal.example"},
		},
		{
			name:       "502 drains body and omits internals",
			status:     http.StatusBadGateway,
			body:       `{"error":"gateway SECRET https://internal.example/x"}`,
			wantSubstr: "HTTP 502",
			forbid:     []string{"SECRET", "https://"},
		},
	}

	addArgs := AddDropRuleArgs{
		Name:    "drop-test-service",
		Filters: []DropRuleFilter{{Key: `attributes["level"]`, Value: "debug"}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)
			cfg := testDropRuleConfig(server.URL)

			addHandler := NewAddDropRuleHandler(server.Client(), cfg)
			_, _, err := addHandler(context.Background(), &mcp.CallToolRequest{}, addArgs)
			if err == nil {
				t.Fatal("add_drop_rule: expected tool error")
			}
			assertDropRuleRedactedError(t, "add_drop_rule", err, tt.wantSubstr, tt.forbid)

			getHandler := NewGetDropRulesHandler(server.Client(), cfg)
			_, _, gerr := getHandler(context.Background(), &mcp.CallToolRequest{}, GetDropRulesArgs{})
			if gerr == nil {
				t.Fatal("get_drop_rules: expected tool error")
			}
			assertDropRuleRedactedError(t, "get_drop_rules", gerr, tt.wantSubstr, tt.forbid)
		})
	}
}

func assertDropRuleRedactedError(t *testing.T, tool string, err error, wantSubstr string, forbid []string) {
	t.Helper()
	got := err.Error()
	if !strings.Contains(got, tool+":") {
		t.Fatalf("%s: error not wrapped: %s", tool, got)
	}
	if wantSubstr != "" && !strings.Contains(got, wantSubstr) {
		t.Fatalf("%s: error %q missing %q", tool, got, wantSubstr)
	}
	for _, f := range forbid {
		if strings.Contains(got, f) {
			t.Fatalf("%s: error leaked %q: %s", tool, f, got)
		}
	}
}

func TestDropRuleHandlerUpstreamErrorBoundsBodySize(t *testing.T) {
	// A 400 body well past utils.UpstreamBodyLimit (512) must be truncated
	// before it reaches the tool error surfaced to the model.
	largeBody := `{"error":"` + strings.Repeat("x", 800) + `"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(largeBody))
	}))
	t.Cleanup(server.Close)
	cfg := testDropRuleConfig(server.URL)

	// A run of 600 x's only exists past the 512-byte cap, so its presence in
	// the error means the body was relayed unbounded.
	tail := strings.Repeat("x", 600)

	addHandler := NewAddDropRuleHandler(server.Client(), cfg)
	_, _, err := addHandler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name:    "drop-test-service",
		Filters: []DropRuleFilter{{Key: `attributes["level"]`, Value: "debug"}},
	})
	if err == nil {
		t.Fatal("add_drop_rule: expected tool error")
	}
	if !strings.Contains(err.Error(), "… (truncated)") {
		t.Fatalf("add_drop_rule: expected truncation marker, got %s", err)
	}
	if strings.Contains(err.Error(), tail) {
		t.Fatalf("add_drop_rule: error relayed body beyond the upstream limit: %s", err)
	}

	getHandler := NewGetDropRulesHandler(server.Client(), cfg)
	_, _, gerr := getHandler(context.Background(), &mcp.CallToolRequest{}, GetDropRulesArgs{})
	if gerr == nil {
		t.Fatal("get_drop_rules: expected tool error")
	}
	if !strings.Contains(gerr.Error(), "… (truncated)") {
		t.Fatalf("get_drop_rules: expected truncation marker, got %s", gerr)
	}
	if strings.Contains(gerr.Error(), tail) {
		t.Fatalf("get_drop_rules: error relayed body beyond the upstream limit: %s", gerr)
	}
}
