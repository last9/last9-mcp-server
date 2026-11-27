package loops

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

func testCfg(baseURL string) models.Config {
	return models.Config{
		APIBaseURL: baseURL,
		OrgSlug:    "acme",
		TokenManager: &auth.TokenManager{
			AccessToken: "tok",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

func TestGetLoopsStatus_Enabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(srv.Close)

	result, _, err := NewGetLoopsStatusHandler(srv.Client(), testCfg(srv.URL))(context.Background(), nil, emptyArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var st map[string]any
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		t.Fatal(err)
	}
	if st["enabled"] != true {
		t.Fatalf("status = %s", text)
	}
}

func TestGetLoopsStatus_NotEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":"LOOPS_NOT_ENABLED"}`)
	}))
	t.Cleanup(srv.Close)

	result, _, err := NewGetLoopsStatusHandler(srv.Client(), testCfg(srv.URL))(context.Background(), nil, emptyArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"enabled":false`) || !strings.Contains(text, "LOOPS_NOT_ENABLED") {
		t.Fatalf("status = %s", text)
	}
}

func TestGetLoop_RequiresID(t *testing.T) {
	result, _, err := NewGetLoopHandler(http.DefaultClient, testCfg("http://unused"))(context.Background(), nil, GetLoopArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error")
	}
}

func TestListLoops_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"aut1","name":"One","enabled":false,"status":"draft","action_mode":"simulated"}]`)
	}))
	t.Cleanup(srv.Close)

	result, _, err := NewListLoopsHandler(srv.Client(), testCfg(srv.URL))(context.Background(), nil, ListLoopsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"count":1`) || !strings.Contains(text, "aut1") {
		t.Fatalf("list = %s", text)
	}
}
