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

	AttachParamHintMiddleware(server)
	attrCache := attributes.NewAttributeCache(backend.Client(), cfg)
	// Register twice to mirror the production description-refresh loop
	// (main.go re-runs registerAllTools every 2h): hints must not duplicate.
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

// failIfSchemaRejected fails the test when the call was rejected by JSON
// schema validation ("unexpected additional properties"). Handler-level
// errors are acceptable — the assertion targets the validation layer only.
func failIfSchemaRejected(t *testing.T, res *mcp.CallToolResult, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "unexpected additional properties") {
		t.Fatalf("call rejected by schema validation: %v", err)
	}
	if res != nil && res.IsError {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok && strings.Contains(tc.Text, "unexpected additional properties") {
				t.Fatalf("call rejected by schema validation: %s", tc.Text)
			}
		}
	}
}

// TestToolCall_ParamAliases reproduces the two wrong-param calls observed
// from Cursor agents: `match` instead of `match_query`, and `service` on
// get_service_summary which declared no service parameter at all.
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
			Arguments: map[string]any{"service": "cartlyst-python"},
		})
		failIfSchemaRejected(t, res, err)
	})

	t.Run("get_service_summary accepts service_name", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_service_summary",
			Arguments: map[string]any{"service_name": "cartlyst-python"},
		})
		failIfSchemaRejected(t, res, err)
	})
}

// TestToolCall_UnknownParamHint asserts that genuinely unknown parameters
// are still rejected (additionalProperties stays strict) and that the error
// is recoverable: it names valid parameters and offers a did-you-mean
// suggestion when an unambiguous near-match exists.
func TestToolCall_UnknownParamHint(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	t.Run("near-match key gets one suggestion", func(t *testing.T) {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_service_summary",
			Arguments: map[string]any{"servicename": "cartlyst-python"},
		})
		if err == nil {
			t.Fatal("expected schema validation error for unknown key, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "unexpected additional properties") {
			t.Fatalf("expected strict validation to stay in force, got: %s", msg)
		}
		if !strings.Contains(msg, `did you mean "service_name"`) {
			t.Fatalf("expected did-you-mean suggestion for service_name, got: %s", msg)
		}
		if !strings.Contains(msg, "Valid parameters") {
			t.Fatalf("expected valid parameter list in error, got: %s", msg)
		}
		if n := strings.Count(msg, "Valid parameters"); n != 1 {
			t.Fatalf("hint duplicated %d times (middleware stacked across re-registration?): %s", n, msg)
		}
	})

	t.Run("no near-match key gets param list without suggestion", func(t *testing.T) {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_service_summary",
			Arguments: map[string]any{"frobnicate": 1},
		})
		if err == nil {
			t.Fatal("expected schema validation error for unknown key, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "Valid parameters") {
			t.Fatalf("expected valid parameter list in error, got: %s", msg)
		}
		if strings.Contains(msg, "did you mean") {
			t.Fatalf("expected no suggestion for a far-off key, got: %s", msg)
		}
	})
}
