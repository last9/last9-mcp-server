package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"last9-mcp/internal/knowledge"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestKnowledgeStore creates a temporary SQLite knowledge store for testing.
func newTestKnowledgeStore(t *testing.T) (knowledge.Store, func()) {
	t.Helper()
	tmpDB := "test_prompts.db"
	store, err := knowledge.NewStore(tmpDB)
	if err != nil {
		t.Fatalf("failed to init knowledge store: %v", err)
	}
	return store, func() {
		store.Close()
		os.Remove(tmpDB)
		os.Remove(tmpDB + "-shm")
		os.Remove(tmpDB + "-wal")
	}
}

// makeRequest constructs a GetPromptRequest with the given arguments.
func makeRequest(name string, args map[string]string) *mcp.GetPromptRequest {
	return &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Name:      name,
			Arguments: args,
		},
	}
}

func TestK8sInfraPrompt_NoArgs(t *testing.T) {
	handler := makePromptHandler(promptDefs[0], nil)
	result, err := handler(context.Background(), makeRequest("k8s-infra-analysis", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without KG store and no service_name: should have reference message + workflow message
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}

	// First message: reference material (assistant role)
	if result.Messages[0].Role != mcp.Role("assistant") {
		t.Errorf("expected assistant role for reference message, got %s", result.Messages[0].Role)
	}
	refText := result.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(refText, "Reference Material") {
		t.Error("reference message should contain 'Reference Material' header")
	}
	// Should include prometheus queries reference
	if !strings.Contains(refText, "container_cpu_usage_seconds_total") {
		t.Error("reference message should contain PromQL queries from prometheus-k8s-queries.md")
	}
	// Should include investigation framework
	if !strings.Contains(refText, "Tool Selection Priority") {
		t.Error("reference message should contain investigation framework content")
	}

	// Second message: workflow (user role)
	if result.Messages[1].Role != mcp.Role("user") {
		t.Errorf("expected user role for workflow message, got %s", result.Messages[1].Role)
	}
	wfText := result.Messages[1].Content.(*mcp.TextContent).Text
	if !strings.Contains(wfText, "K8s Infrastructure Analysis") {
		t.Error("workflow message should contain 'K8s Infrastructure Analysis'")
	}
	// Placeholders should remain when no args provided
	if !strings.Contains(wfText, "$SERVICE_NAME") {
		t.Error("workflow should still contain $SERVICE_NAME placeholder when no args given")
	}
}

func TestAppPerformancePrompt_NoArgs(t *testing.T) {
	handler := makePromptHandler(promptDefs[1], nil)
	result, err := handler(context.Background(), makeRequest("app-performance-analysis", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}

	refText := result.Messages[0].Content.(*mcp.TextContent).Text
	// Should include APM tool patterns
	if !strings.Contains(refText, "Tool Selection Decision Tree") {
		t.Error("reference message should contain APM tool patterns content")
	}
	// Should include investigation framework
	if !strings.Contains(refText, "Tool Selection Priority") {
		t.Error("reference message should contain investigation framework content")
	}
	// Should NOT include the K8s-specific query catalog (HPA, Node Conditions sections)
	if strings.Contains(refText, "Horizontal Pod Autoscaler") {
		t.Error("app performance prompt should not include K8s-specific HPA query catalog")
	}

	wfText := result.Messages[1].Content.(*mcp.TextContent).Text
	if !strings.Contains(wfText, "App Performance Analysis") {
		t.Error("workflow message should contain 'App Performance Analysis'")
	}
}

func TestIncidentRCAPrompt_NoArgs(t *testing.T) {
	handler := makePromptHandler(promptDefs[2], nil)
	result, err := handler(context.Background(), makeRequest("incident-rca", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}

	refText := result.Messages[0].Content.(*mcp.TextContent).Text
	// Should include all three refs: investigation framework, APM tool patterns, RCA note template
	if !strings.Contains(refText, "Tool Selection Priority") {
		t.Error("reference message should contain investigation framework content")
	}
	if !strings.Contains(refText, "Tool Selection Decision Tree") {
		t.Error("reference message should contain APM tool patterns content")
	}
	if !strings.Contains(refText, "RCA Note Template") {
		t.Error("reference message should contain RCA note template content")
	}

	wfText := result.Messages[1].Content.(*mcp.TextContent).Text
	if !strings.Contains(wfText, "Incident RCA") {
		t.Error("workflow message should contain 'Incident RCA'")
	}
}

func TestArgSubstitution(t *testing.T) {
	handler := makePromptHandler(promptDefs[1], nil)
	args := map[string]string{
		"service_name": "payment-svc",
		"environment":  "production",
		"start_time":   "2024-01-15T14:00:00Z",
		"end_time":     "2024-01-15T15:00:00Z",
	}
	result, err := handler(context.Background(), makeRequest("app-performance-analysis", args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Workflow message is the last one
	wfText := result.Messages[len(result.Messages)-1].Content.(*mcp.TextContent).Text

	// $SERVICE_NAME should be replaced
	if strings.Contains(wfText, "$SERVICE_NAME") {
		t.Error("$SERVICE_NAME should be replaced with 'payment-svc'")
	}
	if !strings.Contains(wfText, "payment-svc") {
		t.Error("workflow should contain substituted service name 'payment-svc'")
	}

	// $ENVIRONMENT should be replaced
	if strings.Contains(wfText, "$ENVIRONMENT") {
		t.Error("$ENVIRONMENT should be replaced with 'production'")
	}
	if !strings.Contains(wfText, "production") {
		t.Error("workflow should contain substituted environment 'production'")
	}

	// $START_TIME should be replaced
	if strings.Contains(wfText, "$START_TIME") {
		t.Error("$START_TIME should be replaced")
	}
	if !strings.Contains(wfText, "2024-01-15T14:00:00Z") {
		t.Error("workflow should contain substituted start_time")
	}

	// $END_TIME should be replaced
	if strings.Contains(wfText, "$END_TIME") {
		t.Error("$END_TIME should be replaced")
	}
	if !strings.Contains(wfText, "2024-01-15T15:00:00Z") {
		t.Error("workflow should contain substituted end_time")
	}
}

func TestK8sPrompt_NamespaceSubstitution(t *testing.T) {
	handler := makePromptHandler(promptDefs[0], nil)
	args := map[string]string{
		"service_name": "checkout",
		"namespace":    "prod-services",
	}
	result, err := handler(context.Background(), makeRequest("k8s-infra-analysis", args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wfText := result.Messages[len(result.Messages)-1].Content.(*mcp.TextContent).Text

	if strings.Contains(wfText, "$NAMESPACE") {
		t.Error("$NAMESPACE should be replaced with 'prod-services'")
	}
	if !strings.Contains(wfText, "prod-services") {
		t.Error("workflow should contain substituted namespace 'prod-services'")
	}
}

func TestKGContextPreloading(t *testing.T) {
	store, cleanup := newTestKnowledgeStore(t)
	defer cleanup()

	ctx := context.Background()

	// Seed the store with some data
	if err := store.IngestNodes(ctx, []knowledge.Node{
		{ID: "svc:payment", Type: "Service", Name: "payment-service"},
		{ID: "db:pg:payments", Type: "Database", Name: "payments-db"},
	}); err != nil {
		t.Fatalf("seed IngestNodes: %v", err)
	}
	if err := store.IngestEdges(ctx, []knowledge.Edge{
		{SourceID: "svc:payment", TargetID: "db:pg:payments", Relation: "CALLS"},
	}); err != nil {
		t.Fatalf("seed IngestEdges: %v", err)
	}

	handler := makePromptHandler(promptDefs[1], store)
	args := map[string]string{"service_name": "payment"}
	result, err := handler(ctx, makeRequest("app-performance-analysis", args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With KG data: should have KG context message + reference message + workflow message = 3
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages (KG context + reference + workflow), got %d", len(result.Messages))
	}

	// First message: KG context (assistant role)
	kgMsg := result.Messages[0]
	if kgMsg.Role != mcp.Role("assistant") {
		t.Errorf("expected assistant role for KG context, got %s", kgMsg.Role)
	}
	kgText := kgMsg.Content.(*mcp.TextContent).Text
	if !strings.Contains(kgText, "Prior Knowledge Graph Context") {
		t.Error("KG context message should contain 'Prior Knowledge Graph Context'")
	}
	if !strings.Contains(kgText, "payment") {
		t.Error("KG context message should reference 'payment'")
	}
}

func TestKGContextPreloading_NoResults(t *testing.T) {
	store, cleanup := newTestKnowledgeStore(t)
	defer cleanup()

	// Don't seed any data — store is empty
	handler := makePromptHandler(promptDefs[1], store)
	args := map[string]string{"service_name": "nonexistent-service"}
	result, err := handler(context.Background(), makeRequest("app-performance-analysis", args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty KG: should have reference message + workflow message = 2 (no KG context)
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (no KG context), got %d", len(result.Messages))
	}
}

func TestPromptDescriptions(t *testing.T) {
	for _, def := range promptDefs {
		if def.prompt.Description == "" {
			t.Errorf("prompt %q has empty description", def.prompt.Name)
		}
		if def.prompt.Name == "" {
			t.Error("found prompt with empty name")
		}
		if def.prompt.Title == "" {
			t.Errorf("prompt %q has empty title", def.prompt.Name)
		}
	}
}

func TestSubstituteArgs(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		args     map[string]string
		argNames []string
		want     string
	}{
		{
			name:     "basic substitution",
			text:     "service=$SERVICE_NAME env=$ENVIRONMENT",
			args:     map[string]string{"service_name": "foo", "environment": "prod"},
			argNames: []string{"service_name", "environment"},
			want:     "service=foo env=prod",
		},
		{
			name:     "missing arg leaves placeholder",
			text:     "service=$SERVICE_NAME env=$ENVIRONMENT",
			args:     map[string]string{"service_name": "foo"},
			argNames: []string{"service_name", "environment"},
			want:     "service=foo env=$ENVIRONMENT",
		},
		{
			name:     "empty arg leaves placeholder",
			text:     "service=$SERVICE_NAME",
			args:     map[string]string{"service_name": ""},
			argNames: []string{"service_name"},
			want:     "service=$SERVICE_NAME",
		},
		{
			name:     "nil args leaves all placeholders",
			text:     "service=$SERVICE_NAME",
			args:     nil,
			argNames: []string{"service_name"},
			want:     "service=$SERVICE_NAME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substituteArgs(tt.text, tt.args, tt.argNames)
			if got != tt.want {
				t.Errorf("substituteArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}
