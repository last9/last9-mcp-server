package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

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

func registeredToolNames(t *testing.T, cfg models.Config) map[string]*mcp.Tool {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("test-last9-mcp", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	if err := registerAllTools(server, cfg); err != nil {
		t.Fatal(err)
	}
	clientSession := connectToolsClient(t, server)
	list, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]*mcp.Tool, len(list.Tools))
	for _, tool := range list.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func connectToolsClient(t *testing.T, server *last9mcp.Last9MCPServer) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

func TestRegisterAllToolsPulseManageFailsClosedByDefault(t *testing.T) {
	tools := registeredToolNames(t, testToolRegistrationConfig())
	if _, ok := tools["get_pulse_report"]; !ok {
		t.Fatal("default surface should include Pulse reads")
	}
	for _, name := range []string{"create_pulse_subscription", "update_pulse_subscription", "enable_pulse_subscription", "disable_pulse_subscription", "write_pulse_disposition"} {
		if _, ok := tools[name]; ok {
			t.Errorf("default surface unexpectedly includes managed tool %q", name)
		}
	}
}

func TestRegisterAllToolsPulseManageRequiresExplicitToolset(t *testing.T) {
	allowed, err := toolsets.Parse("pulse_manage")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testToolRegistrationConfig()
	cfg.AllowedTools = allowed
	tools := registeredToolNames(t, cfg)
	for _, name := range []string{"create_pulse_subscription", "update_pulse_subscription", "enable_pulse_subscription", "disable_pulse_subscription", "write_pulse_disposition"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("pulse_manage missing %q", name)
		}
	}
	if _, ok := tools["get_pulse_report"]; ok {
		t.Fatal("pulse_manage must not implicitly expose Pulse reads")
	}
}

func TestPulseSchemasDoNotAcceptOrganizationScope(t *testing.T) {
	tools := registeredToolNames(t, testToolRegistrationConfig())
	for _, name := range []string{"list_pulse_runs", "get_pulse_report", "list_pulse_evidence"} {
		schema := schemaAsMap(t, tools[name].InputSchema)
		properties, _ := schema["properties"].(map[string]any)
		if _, exists := properties["organization_id"]; exists {
			t.Errorf("%s accepts caller-supplied organization_id", name)
		}
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

func TestRegisterIfAllowedConvertsSchemaPanicToError(t *testing.T) {
	server, err := last9mcp.NewServerWithOptions("test-last9-mcp", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	type badIn struct {
		Ch chan int `json:"ch"`
	}
	err = registerIfAllowed(server, nil, &mcp.Tool{Name: "bad_tool", Description: "bad"}, func(_ context.Context, _ *mcp.CallToolRequest, _ badIn) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	})
	if err == nil {
		t.Fatal("expected registration error for invalid tool schema")
	}
	if !strings.Contains(err.Error(), "bad_tool") {
		t.Fatalf("error should name tool: %v", err)
	}
}

func TestRegisterAllTools_ExposesDashboardObjectSchemas(t *testing.T) {
	server, err := last9mcp.NewServerWithOptions("test-last9-mcp", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	cfg := testToolRegistrationConfig()
	if err := registerAllTools(server, cfg); err != nil {
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

	for _, name := range []string{"list_dashboard_snapshots", "get_dashboard_snapshot", "delete_dashboard_snapshot"} {
		_ = toolByName(t, list.Tools, name)
	}
}
