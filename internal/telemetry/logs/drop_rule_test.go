package logs

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
			{Key: "service_name", Value: "test-service", Operator: "equals", Conjunction: "and"},
		},
	})

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestAddDropRuleHandlerSendsCorrectPayload(t *testing.T) {
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

	handler := NewAddDropRuleHandler(server.Client(), testDropRuleConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AddDropRuleArgs{
		Name: "drop-test-service",
		Filters: []DropRuleFilter{
			{Key: "service_name", Value: "test-service", Operator: "equals", Conjunction: "and"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if capturedMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", capturedMethod)
	}
	if capturedBody["name"] != "drop-test-service" {
		t.Fatalf("expected name=drop-test-service, got %v", capturedBody["name"])
	}
	if capturedBody["telemetry"] != TELEMETRY_LOGS {
		t.Fatalf("expected telemetry=%s, got %v", TELEMETRY_LOGS, capturedBody["telemetry"])
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
				Filters: []DropRuleFilter{{Key: "service_name", Value: "svc"}},
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
					{Key: "service_name", Value: "svc", Operator: "contains"},
				},
			},
		},
		{
			name: "invalid conjunction",
			args: AddDropRuleArgs{
				Name: "my-rule",
				Filters: []DropRuleFilter{
					{Key: "service_name", Value: "svc", Conjunction: "or"},
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
				Filters: []DropRuleFilter{{Key: "service_name"}},
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
