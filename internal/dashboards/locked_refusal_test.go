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

const lockedRefusalBody = `{"message":"dashboard is locked; ask its author or an admin to unlock it"}`

func lockingServer(t *testing.T, authorID, refusal string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"dashboard":{"id":"uuid-1","name":"Checkout","panels":[],"readonly":true,"created_by":"`+authorID+`"}}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, refusal)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callDashboardWrite(t *testing.T, tool string, srv *httptest.Server) error {
	t.Helper()
	ctx, req, cfg := context.Background(), &mcp.CallToolRequest{}, testDashboardConfig(srv.URL)
	var err error
	switch tool {
	case "update_dashboard":
		_, _, err = NewUpdateDashboardHandler(srv.Client(), cfg)(ctx, req, UpdateDashboardArgs{
			ID: "uuid-1", DashboardRequest: DashboardRequest{Dashboard: json.RawMessage(`{"name":"Checkout","panels":[]}`)},
		})
	case "patch_dashboard":
		_, _, err = NewPatchDashboardHandler(srv.Client(), cfg)(ctx, req, PatchDashboardArgs{
			ID: "uuid-1", Ops: []PatchDashboardOp{{Op: "set_title", Title: "Checkout v2"}},
		})
	case "delete_dashboard":
		_, _, err = NewDeleteDashboardHandler(srv.Client(), cfg)(ctx, req, DeleteDashboardArgs{ID: "uuid-1"})
	}
	return err
}

var dashboardWriteTools = []string{"update_dashboard", "patch_dashboard", "delete_dashboard"}

func TestDashboardWrites_LockedRefusalNamesAuthor(t *testing.T) {
	for _, tool := range dashboardWriteTools {
		t.Run(tool, func(t *testing.T) {
			err := callDashboardWrite(t, tool, lockingServer(t, "user-7", lockedRefusalBody))
			if err == nil || !strings.HasPrefix(err.Error(), "dashboard is locked") || !strings.Contains(err.Error(), "user-7") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDashboardWrites_BuiltInRefusalIsNotLocked(t *testing.T) {
	for _, tool := range dashboardWriteTools {
		t.Run(tool, func(t *testing.T) {
			err := callDashboardWrite(t, tool, lockingServer(t, "user-7", `{"message":"built-in dashboards are read-only"}`))
			if err == nil || strings.HasPrefix(err.Error(), "dashboard is locked") || !strings.Contains(err.Error(), "readonly") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
