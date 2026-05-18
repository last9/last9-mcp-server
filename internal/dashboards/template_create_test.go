package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListDashboardTemplatesHandler_ReturnsK8sRightsizing(t *testing.T) {
	handler := NewListDashboardTemplatesHandler()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardTemplatesArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !json.Valid([]byte(text)) {
		t.Fatalf("expected JSON, got: %s", text)
	}
	if !strings.Contains(text, "k8s-rightsizing") {
		t.Fatalf("expected k8s-rightsizing in catalog, got: %s", text)
	}
}

func TestCreateDashboardFromTemplateHandler_POSTsRenderedBody(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/dashboards/") {
			http.NotFound(w, r)
			return
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"dashboard":{"id":"new-tmpl-id","name":"Golden — K8s Rightsizing"}}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	handler := NewCreateDashboardFromTemplateHandler(srv.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardFromTemplateArgs{
		TemplateID: "k8s-rightsizing",
		Knobs: map[string]string{
			"DASHBOARD_NAME": "Golden — K8s Rightsizing",
			"NAMESPACES":     "prod|staging",
			"CLUSTERS":       ".*",
			"WINDOW":         "360",
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !json.Valid(capturedBody) {
		t.Fatalf("POSTed body is not valid JSON: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), "k8s-rightsizing") && !strings.Contains(string(capturedBody), "container_cpu_usage") {
		t.Fatalf("POSTed body missing expected template content: %s", capturedBody)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "new-tmpl-id") {
		t.Fatalf("expected dashboard id in response, got: %s", text)
	}
	if result.Meta["reference_url"] == nil {
		t.Fatalf("expected reference_url in meta, got: %v", result.Meta)
	}
}

func TestCreateDashboardFromTemplateHandler_UnknownTemplate(t *testing.T) {
	cfg := testDashboardConfig("http://localhost")
	handler := NewCreateDashboardFromTemplateHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardFromTemplateArgs{
		TemplateID: "does-not-exist",
		Knobs:      map[string]string{},
	})
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}
