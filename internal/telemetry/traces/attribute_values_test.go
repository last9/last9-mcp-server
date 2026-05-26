package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestCfg(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestGetTraceAttributeValuesHandler_EmptyTagName(t *testing.T) {
	handler := NewGetTraceAttributeValuesHandler(http.DefaultClient, models.Config{})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: ""})
	if err == nil {
		t.Fatal("expected error for empty tag_name, got nil")
	}
	if !strings.Contains(err.Error(), "tag_name is required") {
		t.Errorf("expected 'tag_name is required', got: %v", err)
	}
}

func TestGetTraceAttributeValuesHandler_WhitespaceOnlyTagName(t *testing.T) {
	// After normalizeTagName("   ") the result is "", which should be rejected.
	handler := NewGetTraceAttributeValuesHandler(http.DefaultClient, models.Config{})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only tag_name, got nil")
	}
	if !strings.Contains(err.Error(), "blank") {
		t.Errorf("expected 'cannot be blank', got: %v", err)
	}
}

func TestGetTraceAttributeValuesHandler_NonSuccessAPIStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"error","data":null}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err == nil {
		t.Fatal("expected error for non-success API status, got nil")
	}
	if !strings.Contains(err.Error(), "non-success status") {
		t.Errorf("expected non-success status error, got: %v", err)
	}
}

func TestGetTraceAttributeValuesHandler_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Must be POST with pipeline body.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("expected valid JSON body, got: %v", err)
		}
		if _, ok := body["pipeline"]; !ok {
			t.Errorf("expected 'pipeline' key in body, got: %v", body)
		}
		// region, start, end must be query params.
		if r.URL.Query().Get("start") == "" {
			t.Errorf("expected start query param")
		}
		if r.URL.Query().Get("end") == "" {
			t.Errorf("expected end query param")
		}
		if !strings.Contains(r.URL.Path, "http.method") {
			t.Errorf("expected tag name in path, got: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["GET","POST","PUT"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected non-empty result content")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "GET") {
		t.Errorf("expected values in response, got: %s", text)
	}
	if !strings.Contains(text, `attributes['http.method']`) {
		t.Errorf("expected filter_field in response, got: %s", text)
	}
}

func TestGetTraceAttributeValuesHandler_ResourceTagNormalized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// resources['department'] normalizes to resource_department in the URL path.
		if !strings.Contains(r.URL.Path, "resource_department") {
			t.Errorf("expected resource_department in path, got: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["engineering","platform"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "resources['department']"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
