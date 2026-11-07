package dashboards

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func boolPtr(b bool) *bool { return &b }

func TestSetDashboardLockHandler_LocksAndUnlocks(t *testing.T) {
	for _, locked := range []bool{true, false} {
		var gotMethod, gotPath, gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			if locked {
				_, _ = io.WriteString(w, `{"id":"uuid-1","readonly":true}`)
			} else {
				_, _ = io.WriteString(w, `{"id":"uuid-1","readonly":false}`)
			}
		}))

		handler := NewSetDashboardLockHandler(srv.Client(), testDashboardConfig(srv.URL))
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SetDashboardLockArgs{ID: "uuid-1", Locked: boolPtr(locked)})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if gotMethod != http.MethodPatch || gotPath != "/dashboards/uuid-1/readonly" {
			t.Fatalf("request %s %s", gotMethod, gotPath)
		}
		wantBody, wantText := `{"readonly":false}`, `{"id":"uuid-1","locked":false}`
		if locked {
			wantBody, wantText = `{"readonly":true}`, `{"id":"uuid-1","locked":true}`
		}
		if gotBody != wantBody {
			t.Errorf("body %s, want %s", gotBody, wantBody)
		}
		if text := result.Content[0].(*mcp.TextContent).Text; text != wantText {
			t.Errorf("text %s, want %s", text, wantText)
		}
		if ref := result.Meta["reference_url"]; ref != "/v2/organizations/test-org/dashboards/uuid-1" {
			t.Errorf("reference_url %v", ref)
		}
	}
}

func TestSetDashboardLockHandler_MapsRefusals(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"not author or admin", http.StatusForbidden, `{"message":"only admins or the dashboard author can lock or unlock it"}`,
			"only the author or an org admin can lock or unlock this dashboard"},
		{"built-in dashboard", http.StatusForbidden, `{"message":"built-in dashboards are read-only"}`,
			"built-in dashboards are always read-only and cannot be locked or unlocked"},
		{"not found", http.StatusNotFound, `{"message":"dashboard not found"}`,
			`dashboard "uuid-1" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			handler := NewSetDashboardLockHandler(srv.Client(), testDashboardConfig(srv.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SetDashboardLockArgs{ID: "uuid-1", Locked: boolPtr(true)})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSetDashboardLockHandler_RequiresArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewSetDashboardLockHandler(srv.Client(), testDashboardConfig(srv.URL))
	for name, args := range map[string]SetDashboardLockArgs{
		"id":     {Locked: boolPtr(true)},
		"locked": {ID: "uuid-1"},
	} {
		if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("missing %s: err = %v", name, err)
		}
	}
}

func TestSetDashboardLockHandler_ResponseWithoutStateIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"uuid-1"}`)
	}))
	defer srv.Close()

	handler := NewSetDashboardLockHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SetDashboardLockArgs{ID: "uuid-1", Locked: boolPtr(true)})
	if err == nil || !strings.Contains(err.Error(), "get_dashboard") {
		t.Fatalf("err = %v", err)
	}
}
