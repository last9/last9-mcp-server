package grafana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func grafanaLinkURLParam(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	raw, ok := res.Meta["reference_url"].(string)
	if !ok || raw == "" {
		t.Fatalf("missing reference_url in meta: %v", res.Meta)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse reference_url %q: %v", raw, err)
	}
	if u.Path != "/v2/organizations/acme/grafana" {
		t.Fatalf("path = %q", u.Path)
	}
	return u.Query().Get("url")
}

func grafanaLinkConfigServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/folders/"):
			_, _ = w.Write([]byte(`{"id":7,"uid":"fld 1","title":"Core Payments"}`))
		case r.URL.Path == "/api/folders":
			_, _ = w.Write([]byte(sampleFolders))
		case strings.HasPrefix(r.URL.Path, "/api/dashboards/uid/"):
			_, _ = w.Write([]byte(sampleDashboard))
		default:
			_, _ = w.Write([]byte(sampleSearch))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGrafanaHandlers_GrafanaLink(t *testing.T) {
	srv := grafanaLinkConfigServer(t)
	cfg := testGrafanaConfig(srv.URL)
	cfg.OrgSlug = "acme"
	ctx := context.Background()
	req := &mcp.CallToolRequest{}

	cases := []struct {
		name string
		run  func() (*mcp.CallToolResult, any, error)
		want string
	}{
		{"search", func() (*mcp.CallToolResult, any, error) {
			return NewSearchDashboardsHandler(srv.Client(), cfg)(ctx, req, SearchDashboardsArgs{Query: "payments"})
		}, "/api/gp/dashboards"},
		{"list_folders", func() (*mcp.CallToolResult, any, error) {
			return NewListFoldersHandler(srv.Client(), cfg)(ctx, req, ListFoldersArgs{})
		}, "/api/gp/dashboards"},
		{"list_folder_dashboards", func() (*mcp.CallToolResult, any, error) {
			return NewListFolderDashboardsHandler(srv.Client(), cfg)(ctx, req, ListFolderDashboardsArgs{FolderUID: "fld 1"})
		}, "/api/gp/dashboards/f/fld%201"},
		{"get_dashboard", func() (*mcp.CallToolResult, any, error) {
			return NewGetDashboardHandler(srv.Client(), cfg)(ctx, req, GetDashboardArgs{UID: "abc/123"})
		}, "/api/gp/d/abc%2F123"},
		{"get_dashboard_full_json", func() (*mcp.CallToolResult, any, error) {
			return NewGetDashboardHandler(srv.Client(), cfg)(ctx, req, GetDashboardArgs{UID: "abc123", FullJSON: true})
		}, "/api/gp/d/abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			if got := grafanaLinkURLParam(t, res); got != tc.want {
				t.Fatalf("url = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGrafanaHandlers_NoLinkOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := testGrafanaConfig(srv.URL)
	cfg.OrgSlug = "acme"

	res, _, err := NewGetDashboardHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{UID: "abc123"})
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if res != nil {
		t.Fatalf("error must not return a result with a link: %+v", res)
	}
}
