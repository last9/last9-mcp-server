package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReadQueryFailureKeepsRecoveryGuidance(t *testing.T) {
	for _, mode := range []string{"gateway", "deadline", "success"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				calls.Add(1)
				switch mode {
				case "gateway":
					w.WriteHeader(http.StatusBadGateway)
				case "deadline":
					<-r.Context().Done()
				default:
					_, _ = io.WriteString(w, `{"discover":{"databases":[]}}`)
				}
			}))
			defer upstream.Close()
			session := connectToolFixture(t, registeredToolFixture{
				tool:    &mcp.Tool{Name: "get_databases", Annotations: readOnlyTool("Get Databases")},
				timeout: 100 * time.Millisecond,
			}, apm.NewGetDatabasesHandler(upstream.Client(), writeToolTestConfig(upstream.URL)))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_databases", Arguments: map[string]any{"lookback_minutes": 60}})
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d, want one real request", calls.Load())
			}
			text := toolResultText(result)
			if mode == "success" {
				if result.IsError {
					t.Fatalf("successful discovery failed: %s", text)
				}
				return
			}
			if !result.IsError {
				t.Fatalf("want upstream error, got %s", text)
			}
			if strings.Contains(text, "Do not retry or resend") {
				t.Errorf("read-only discovery must permit recovery: %s", text)
			}
			if result.Meta[utils.ErrorTypeMetaKey] != utils.SystemErrorType {
				t.Errorf("upstream read failure lost system_error classification: %v", result.Meta)
			}
			if mode == "deadline" && !strings.Contains(text, "Narrow the time range") {
				t.Errorf("missing read deadline recovery advice: %s", text)
			}
		})
	}
}
