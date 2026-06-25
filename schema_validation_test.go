package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestSession spins up the full MCP server with all tools registered and
// connects a client over in-memory transports, so calls exercise the SDK's
// schema validation layer (which direct handler invocation bypasses).
func newTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "[]")
	}))
	t.Cleanup(backend.Close)

	cfg := models.Config{
		APIBaseURL: backend.URL,
		Region:     "us-east-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token-for-testing",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	server, err := last9mcp.NewServerWithOptions("last9-mcp-test", "0.0.0", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	attrCache := attributes.NewAttributeCache(backend.Client(), cfg)
	// Register twice to mirror the production description-refresh loop
	// (main.go re-runs registerAllTools every 2h): re-registration must
	// stay idempotent.
	for i := 0; i < 2; i++ {
		if err := registerAllTools(server, cfg, attrCache); err != nil {
			t.Fatalf("failed to register tools: %v", err)
		}
	}

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("failed to connect server: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// schemaRejection returns the validation-rejection text for a call, from
// either delivery path (a thrown error, or a recoverable isError result —
// go-sdk v1.5.0+ returns schema-validation failures as recoverable isError
// tool results per SEP-1303), or "" when the call was not schema-rejected.
func schemaRejection(res *mcp.CallToolResult, err error) string {
	if err != nil && strings.Contains(err.Error(), "unexpected additional properties") {
		return err.Error()
	}
	if res != nil && res.IsError {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok && strings.Contains(tc.Text, "unexpected additional properties") {
				return tc.Text
			}
		}
	}
	return ""
}

// failIfSchemaRejected fails the test when the call was rejected by JSON
// schema validation ("unexpected additional properties"). Handler-level
// errors are acceptable — the assertion targets the validation layer only.
func failIfSchemaRejected(t *testing.T, res *mcp.CallToolResult, err error) {
	t.Helper()
	if msg := schemaRejection(res, err); msg != "" {
		t.Fatalf("call rejected by schema validation: %s", msg)
	}
}

// TestToolCall_ParamAliases reproduces the two wrong-param calls observed
// from agents: `match` instead of `match_query`, and `service` on
// get_service_summary which declared no service parameter at all. Both must
// be accepted via aliases rather than schema-rejected.
func TestToolCall_ParamAliases(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	t.Run("prometheus_label_values accepts match alias", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "prometheus_label_values",
			Arguments: map[string]any{
				"label": "service",
				"match": `{l9_event_name="last9_scheduled_search"}`,
			},
		})
		failIfSchemaRejected(t, res, err)
	})

	t.Run("prometheus_labels accepts match alias", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "prometheus_labels",
			Arguments: map[string]any{
				"match": `{l9_event_name="last9_scheduled_search"}`,
			},
		})
		failIfSchemaRejected(t, res, err)
	})

	t.Run("get_service_summary accepts service", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_service_summary",
			Arguments: map[string]any{"service": "checkout-service"},
		})
		failIfSchemaRejected(t, res, err)
	})

	t.Run("get_service_summary accepts service_name", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_service_summary",
			Arguments: map[string]any{"service_name": "checkout-service"},
		})
		failIfSchemaRejected(t, res, err)
	})
}

// TestToolCall_UnknownParamRejected asserts that genuinely unknown parameters
// stay strictly rejected (additionalProperties: false) and that the rejection
// is recoverable — surfaced as an isError tool result (or error) the client
// can read and self-correct from, naming the offending key.
func TestToolCall_UnknownParamRejected(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_service_summary",
		Arguments: map[string]any{"servicename": "checkout-service"},
	})
	msg := schemaRejection(res, err)
	if msg == "" {
		t.Fatalf("expected unknown key to be schema-rejected, got res=%+v err=%v", res, err)
	}
	if !strings.Contains(msg, "servicename") {
		t.Fatalf("expected rejection to name the offending key, got: %s", msg)
	}
}
