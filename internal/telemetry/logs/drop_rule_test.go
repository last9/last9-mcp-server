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

func TestAddDropRuleHandlerContextCancellation(t *testing.T) {
	// Server that blocks — proves context cancellation aborts the request.
	blocked := make(chan struct{})
	defer close(blocked)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

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
	if !strings.Contains(err.Error(), "405") {
		t.Fatalf("expected status in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "method not allowed") {
		t.Fatalf("expected response body in error, got %v", err)
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
		{
			name: "invalid filter key format",
			args: AddDropRuleArgs{
				Name: "my-rule",
				Filters: []DropRuleFilter{
					{Key: "service_name", Value: "svc"},
				},
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
