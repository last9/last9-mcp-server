package remapping

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testRemappingConfig(actionURL string) models.Config {
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

func TestGetRemappingRulesHandlerContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := NewGetRemappingRulesHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(ctx, &mcp.CallToolRequest{}, GetRemappingRulesArgs{RuleType: ruleTypeLogsExtract})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestGetRemappingRulesHandlerRequiresRuleType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for invalid input")
	}))
	defer server.Close()

	handler := NewGetRemappingRulesHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetRemappingRulesArgs{})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestGetRemappingRulesHandlerSendsGETWithRegion(t *testing.T) {
	var capturedMethod, capturedRegion string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedRegion = r.URL.Query().Get("region")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]string{{"id": "rule-1", "name": "test"}})
	}))
	defer server.Close()

	handler := NewGetRemappingRulesHandler(server.Client(), testRemappingConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetRemappingRulesArgs{
		RuleType: ruleTypeLogsMap,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if capturedMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", capturedMethod)
	}
	if capturedRegion != "us-east-1" {
		t.Fatalf("expected region=us-east-1, got %q", capturedRegion)
	}
}

func TestAddRemappingRuleHandlerSendsLogsExtractPayload(t *testing.T) {
	var capturedMethod string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "rule-123"})
	}))
	defer server.Close()

	handler := NewAddRemappingRuleHandler(server.Client(), testRemappingConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddRemappingRuleArgs{
		RuleType:        ruleTypeLogsExtract,
		Name:            "json-severity",
		RemapKeys:       []string{"level"},
		TargetAttribute: "log_attributes",
		ExtractType:     "json",
		Action:          "upsert",
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
	if capturedBody["name"] != "json-severity" {
		t.Fatalf("expected name=json-severity, got %v", capturedBody["name"])
	}
	props, ok := capturedBody["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", capturedBody["properties"])
	}
	if props["type"] != "json" {
		t.Fatalf("expected properties.type=json, got %v", props["type"])
	}
	if props["target_attribute"] != "log_attributes" {
		t.Fatalf("expected target_attribute=log_attributes, got %v", props["target_attribute"])
	}
}

func TestAddRemappingRuleHandlerSendsLogsMapPayload(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "rule-456"})
	}))
	defer server.Close()

	handler := NewAddRemappingRuleHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddRemappingRuleArgs{
		RuleType:        ruleTypeLogsMap,
		Name:            "map-service",
		RemapKeys:       []string{`attributes["service_name"]`, `attributes["app_name"]`},
		TargetAttribute: "service",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	props, ok := capturedBody["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", capturedBody["properties"])
	}
	if props["target_attribute"] != "service" {
		t.Fatalf("expected target_attribute=service, got %v", props["target_attribute"])
	}
}

func TestAddRemappingRuleHandlerSendsTracesMapPayload(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "rule-789"})
	}))
	defer server.Close()

	handler := NewAddRemappingRuleHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddRemappingRuleArgs{
		RuleType:        ruleTypeTracesMap,
		Name:            "map-trace-service",
		RemapKeys:       []string{`resource.attributes["service.name"]`},
		TargetAttribute: "service",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	props, ok := capturedBody["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", capturedBody["properties"])
	}
	if props["target_attribute"] != "service" {
		t.Fatalf("expected target_attribute=service, got %v", props["target_attribute"])
	}
}

func TestAddRemappingRuleHandlerAcceptsValidPatternExtract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "rule-pattern"})
	}))
	defer server.Close()

	handler := NewAddRemappingRuleHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddRemappingRuleArgs{
		RuleType:        ruleTypeLogsExtract,
		Name:            "ansi-severity",
		ExtractType:     "pattern",
		RemapKeys:       []string{`\[(?P<severity>DEBUG|INFO|WARN|ERROR)[ |]`},
		TargetAttribute: "log_attributes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddRemappingRuleHandlerValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for invalid input")
	}))
	defer server.Close()

	handler := NewAddRemappingRuleHandler(server.Client(), testRemappingConfig(server.URL))

	tests := []struct {
		name string
		args AddRemappingRuleArgs
	}{
		{
			name: "missing rule type",
			args: AddRemappingRuleArgs{Name: "rule", RemapKeys: []string{"k"}, TargetAttribute: "service"},
		},
		{
			name: "invalid rule type",
			args: AddRemappingRuleArgs{RuleType: "invalid", Name: "rule", RemapKeys: []string{"k"}, TargetAttribute: "service"},
		},
		{
			name: "logs_extract missing extract_type",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsExtract, Name: "rule",
				RemapKeys: []string{"level"}, TargetAttribute: "log_attributes",
			},
		},
		{
			name: "logs_extract invalid target",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsExtract, Name: "rule", ExtractType: "json",
				RemapKeys: []string{"level"}, TargetAttribute: "service",
			},
		},
		{
			name: "logs_map rejects extract fields",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsMap, Name: "rule", ExtractType: "json",
				RemapKeys: []string{"level"}, TargetAttribute: "service",
			},
		},
		{
			name: "logs_map invalid target",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsMap, Name: "rule",
				RemapKeys: []string{"level"}, TargetAttribute: "log_attributes",
			},
		},
		{
			name: "traces_map invalid target",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeTracesMap, Name: "rule",
				RemapKeys: []string{"svc"}, TargetAttribute: "severity",
			},
		},
		{
			name: "pattern requires single remap key",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsExtract, Name: "rule", ExtractType: "pattern",
				RemapKeys: []string{`(?P<a>\w+)`, `(?P<b>\w+)`}, TargetAttribute: "log_attributes",
			},
		},
		{
			name: "pattern requires valid regex",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsExtract, Name: "rule", ExtractType: "pattern",
				RemapKeys: []string{"[unclosed"}, TargetAttribute: "log_attributes",
			},
		},
		{
			name: "pattern requires named capture group",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsExtract, Name: "rule", ExtractType: "pattern",
				RemapKeys: []string{`\[DEBUG\]`}, TargetAttribute: "log_attributes",
			},
		},
		{
			name: "like precondition requires valid regex",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsExtract, Name: "rule", ExtractType: "json",
				RemapKeys: []string{"level"}, TargetAttribute: "log_attributes",
				Preconditions: []RemappingPrecondition{
					{Key: `attributes["service"]`, Value: "[unclosed", Operator: "like"},
				},
			},
		},
		{
			name: "empty remap key rejected",
			args: AddRemappingRuleArgs{
				RuleType: ruleTypeLogsMap, Name: "rule",
				RemapKeys: []string{" "}, TargetAttribute: "service",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestAddRemappingRuleHandlerContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := NewAddRemappingRuleHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(ctx, &mcp.CallToolRequest{}, AddRemappingRuleArgs{
		RuleType:        ruleTypeLogsExtract,
		Name:            "rule",
		RemapKeys:       []string{"level"},
		TargetAttribute: "log_attributes",
		ExtractType:     "json",
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
