package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testToolRegistrationConfig() models.Config {
	return models.Config{
		APIBaseURL: "http://example.test",
		OrgSlug:    "test-org",
		Region:     "us-east-1",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func schemaAsMap(t *testing.T, schema any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("json.Marshal(schema) error = %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}
	return out
}

func toolByName(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func assertRegisteredDashboardSchema(t *testing.T, tool *mcp.Tool, required []string) {
	t.Helper()

	schema := schemaAsMap(t, tool.InputSchema)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s InputSchema missing properties: %v", tool.Name, schema)
	}

	dashboard, ok := props["dashboard"].(map[string]any)
	if !ok {
		t.Fatalf("%s InputSchema missing dashboard property: %v", tool.Name, props)
	}
	if dashboard["type"] != "object" {
		t.Fatalf("%s dashboard type: want object, got %v", tool.Name, dashboard["type"])
	}

	requiredRaw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("%s InputSchema missing required list: %v", tool.Name, schema)
	}
	requiredSet := make(map[string]bool, len(requiredRaw))
	for _, field := range requiredRaw {
		if s, ok := field.(string); ok {
			requiredSet[s] = true
		}
	}
	for _, field := range required {
		if !requiredSet[field] {
			t.Fatalf("%s required fields missing %q: %v", tool.Name, field, requiredRaw)
		}
	}
}

func TestRegisterAllTools_ExposesDashboardObjectSchemas(t *testing.T) {
	server, err := last9mcp.NewServerWithOptions("test-last9-mcp", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	cfg := testToolRegistrationConfig()
	if err := registerAllTools(server, cfg, attributes.NewAttributeCache(nil, cfg)); err != nil {
		t.Fatal(err)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	list, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	assertRegisteredDashboardSchema(t, toolByName(t, list.Tools, "create_dashboard"), []string{"dashboard"})
	assertRegisteredDashboardSchema(t, toolByName(t, list.Tools, "update_dashboard"), []string{"id", "dashboard"})

	if got, want := schemaAsMap(t, toolByName(t, list.Tools, "create_dashboard").InputSchema), schemaAsMap(t, dashboards.GetCreateDashboardInputSchema()); !reflect.DeepEqual(got, want) {
		t.Fatalf("create_dashboard InputSchema mismatch:\ngot  %v\nwant %v", got, want)
	}
	if got, want := schemaAsMap(t, toolByName(t, list.Tools, "update_dashboard").InputSchema), schemaAsMap(t, dashboards.GetUpdateDashboardInputSchema()); !reflect.DeepEqual(got, want) {
		t.Fatalf("update_dashboard InputSchema mismatch:\ngot  %v\nwant %v", got, want)
	}
}
