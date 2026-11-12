package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetDashboardHandler_RequiresRegion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"dashboard": map[string]any{"id": "uuid-1", "name": "Test"},
		})
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	cfg.Region = ""

	handler := NewGetDashboardHandler(srv.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1", Region: ""})
	if err == nil {
		t.Fatal("expected error for empty region")
	}
}

func TestGetDashboardHandler_SendsRegionQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"dashboard": map[string]any{"id": "uuid-1", "name": "Test"},
		})
	}))
	defer srv.Close()

	handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1", Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "region=us-east-1") {
		t.Fatalf("query %q", gotQuery)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok {
		t.Fatalf("expected reference_url in meta, got %v", result.Meta)
	}
	if refURL != "/v2/organizations/test-org/dashboards/uuid-1" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestGetDashboardHandler_UsesConfiguredRegionDefault(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != fmt.Sprintf("/dashboards/%s", "uuid-1") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"dashboard": map[string]any{"id": "uuid-1"}})
	}))
	defer srv.Close()

	handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "region=us-east-1") {
		t.Fatalf("query %q", gotQuery)
	}
}

func TestGetDashboardHandler_ServesLockStateAndAuthor(t *testing.T) {
	tests := []struct {
		name       string
		dashboard  string
		wantLocked bool
	}{
		{"locked", `{"id":"uuid-1","readonly":true,"created_by":"user-7"}`, true},
		{"unlocked", `{"id":"uuid-1","readonly":false,"created_by":"user-7"}`, false},
		{"omitted", `{"id":"uuid-1","created_by":"user-7"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"dashboard":%s,"metadata":{"_type":"metrics","q":"a<b"}}`, tt.dashboard)
			}))
			defer srv.Close()

			handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1"})
			if err != nil {
				t.Fatal(err)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			var got struct {
				Locked    *bool           `json:"locked"`
				CreatedBy string          `json:"created_by"`
				Dashboard json.RawMessage `json:"dashboard"`
			}
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("%v: %s", err, text)
			}
			if got.Locked == nil || *got.Locked != tt.wantLocked || got.CreatedBy != "user-7" {
				t.Fatalf("locked/created_by wrong: %s", text)
			}
			if string(got.Dashboard) != tt.dashboard || !strings.Contains(text, `"q":"a<b"`) {
				t.Fatalf("dashboard body not preserved: %s", text)
			}
		})
	}
}

func TestGetDashboardHandler_AuthorOmittedIsNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"dashboard":{"id":"uuid-1"}}`))
	}))
	defer srv.Close()

	handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"locked":false`) || !strings.Contains(text, `"created_by":null`) {
		t.Fatalf("text %s", text)
	}
}

func TestGetDashboardHandler_NormalizesEscapedHTMLCharacters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"dashboard":{"id":"uuid-1","name":"QA \u003c\u003e\u0026"}}`))
	}))
	defer srv.Close()

	handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"name":"QA <>&"`) {
		t.Fatalf("HTML characters were not preserved literally: %s", text)
	}
}
