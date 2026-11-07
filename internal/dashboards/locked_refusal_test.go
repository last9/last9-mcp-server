package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	lockedRefusalBody  = `{"message":"dashboard is locked; ask its author or an admin to unlock it"}`
	builtInRefusalBody = `{"message":"built-in dashboards are read-only"}`
	lockedFixture      = `{
	"dashboard":{"id":"uuid-1","name":"Checkout","readonly":true,"created_by":"user-7","panels":[]},
	"metadata":{"_category":"custom","_type":"metrics"}
}`
)

// unknownLockFixture omits readonly, as a dashboard read before the lock was
// taken or from an API that does not report it.
var unknownLockFixture = strings.Replace(lockedFixture, `"readonly":true,`, "", 1)

// lockingServer answers GET with get and every write with status and refusal, counting writes.
type lockingServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	writes int
}

func newLockingServer(t *testing.T, get string, status int, refusal string) *lockingServer {
	t.Helper()
	ls := &lockingServer{}
	ls.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, get)
			return
		}
		ls.mu.Lock()
		ls.writes++
		ls.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, refusal)
	}))
	t.Cleanup(ls.srv.Close)
	return ls
}

func (ls *lockingServer) writeCount() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.writes
}

func callDashboardWrite(t *testing.T, tool string, srv *httptest.Server) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, req, cfg := context.Background(), &mcp.CallToolRequest{}, testDashboardConfig(srv.URL)
	var res *mcp.CallToolResult
	var err error
	switch tool {
	case "update_dashboard":
		res, _, err = NewUpdateDashboardHandler(srv.Client(), cfg)(ctx, req, UpdateDashboardArgs{
			ID: "uuid-1", DashboardRequest: DashboardRequest{Dashboard: json.RawMessage(`{"name":"Checkout","panels":[]}`)},
		})
	case "patch_dashboard":
		res, _, err = NewPatchDashboardHandler(srv.Client(), cfg)(ctx, req, PatchDashboardArgs{
			ID: "uuid-1", Ops: []PatchDashboardOp{{Op: "set_title", Title: "Checkout v2"}},
		})
	case "delete_dashboard":
		res, _, err = NewDeleteDashboardHandler(srv.Client(), cfg)(ctx, req, DeleteDashboardArgs{ID: "uuid-1"})
	}
	return res, err
}

var dashboardWriteTools = []string{"update_dashboard", "patch_dashboard", "delete_dashboard"}

// userErrorText asserts a recoverable tool error (isError, not a system error) and returns its text.
func userErrorText(t *testing.T, res *mcp.CallToolResult, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("want a recoverable tool error, got protocol error %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("want an isError result, got %+v", res)
	}
	if _, system := res.Meta[utils.ErrorTypeMetaKey]; system {
		t.Fatalf("a refused write is a user error, meta = %v", res.Meta)
	}
	return utils.GetTextContent(t, res)
}

func assertLockedText(t *testing.T, text string) {
	t.Helper()
	if !strings.HasPrefix(text, "dashboard is locked") {
		t.Errorf("text %q must start with the stable prefix", text)
	}
	for _, want := range []string{"user-7", "org admin", "set_dashboard_lock", "create_dashboard", "nothing was written"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q missing %q", text, want)
		}
	}
}

func TestDashboardWrites_LockedDashboardIsRefusedWithoutWriting(t *testing.T) {
	for _, tool := range dashboardWriteTools {
		t.Run(tool, func(t *testing.T) {
			ls := newLockingServer(t, lockedFixture, http.StatusOK, `{}`)
			res, err := callDashboardWrite(t, tool, ls.srv)
			assertLockedText(t, userErrorText(t, res, err))
			if n := ls.writeCount(); n != 0 {
				t.Fatalf("sent %d writes to a locked dashboard", n)
			}
		})
	}
}

func TestDashboardWrites_LockedRefusalNamesAuthor(t *testing.T) {
	for _, tool := range dashboardWriteTools {
		t.Run(tool, func(t *testing.T) {
			ls := newLockingServer(t, unknownLockFixture, http.StatusForbidden, lockedRefusalBody)
			res, err := callDashboardWrite(t, tool, ls.srv)
			assertLockedText(t, userErrorText(t, res, err))
			if n := ls.writeCount(); n != 1 {
				t.Fatalf("writes = %d, want 1", n)
			}
		})
	}
}

func TestDashboardWrites_BuiltInRefusalIsNotLocked(t *testing.T) {
	for _, tool := range dashboardWriteTools {
		t.Run(tool, func(t *testing.T) {
			ls := newLockingServer(t, unknownLockFixture, http.StatusForbidden, builtInRefusalBody)
			res, err := callDashboardWrite(t, tool, ls.srv)
			text := userErrorText(t, res, err)
			if strings.HasPrefix(text, "dashboard is locked") || !strings.Contains(text, "readonly") || !strings.Contains(text, "create_dashboard") {
				t.Fatalf("text = %q", text)
			}
		})
	}
}

func TestDashboardWrites_UnlockedDashboardIsWritten(t *testing.T) {
	unlocked := strings.Replace(lockedFixture, `"readonly":true`, `"readonly":false`, 1)
	for _, tool := range dashboardWriteTools {
		t.Run(tool, func(t *testing.T) {
			ls := newLockingServer(t, unlocked, http.StatusOK, `{"dashboard":{"id":"uuid-1"}}`)
			res, err := callDashboardWrite(t, tool, ls.srv)
			if err != nil || res == nil || res.IsError {
				t.Fatalf("res = %+v, err = %v", res, err)
			}
			if n := ls.writeCount(); n != 1 {
				t.Fatalf("writes = %d, want 1", n)
			}
		})
	}
}

func TestDashboardLockOf_DecodesReadonlyAsTriState(t *testing.T) {
	tests := []struct {
		name   string
		doc    map[string]any
		locked bool
		known  bool
	}{
		{"absent", map[string]any{}, false, false},
		{"null", map[string]any{"readonly": nil}, false, false},
		{"non-bool", map[string]any{"readonly": "true"}, false, false},
		{"false", map[string]any{"readonly": false}, false, true},
		{"true", map[string]any{"readonly": true}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := dashboardLockOf(tt.doc)
			if (lock.Readonly != nil) != tt.known {
				t.Fatalf("known = %v, want %v", lock.Readonly != nil, tt.known)
			}
			if lock.locked() != tt.locked {
				t.Fatalf("locked = %v, want %v", lock.locked(), tt.locked)
			}
		})
	}
}

func TestPreviewDashboard_ReportsLockedDashboard(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
		locked  bool
	}{
		{"locked", lockedFixture, true},
		{"unknown", unknownLockFixture, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := previewTestArgs()
			args.ID = "uuid-1"
			args.Ops = []PatchDashboardOp{{Op: "set_title", Title: "x"}}
			srv := newLockingServer(t, tt.fixture, http.StatusOK, `{}`)
			res, _, err := NewPreviewDashboardHandler(srv.srv.Client(), testDashboardConfig(srv.srv.URL))(context.Background(), nil, args)
			if err != nil {
				t.Fatal(err)
			}
			if n := srv.writeCount(); n != 0 {
				t.Fatalf("preview sent %d writes", n)
			}
			var report map[string]any
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, res)), &report); err != nil {
				t.Fatal(err)
			}
			note, _ := report["locked"].(string)
			if tt.locked != (note != "") {
				t.Fatalf("locked = %v, report = %v", report["locked"], report)
			}
			if tt.locked && (!strings.Contains(note, "user-7") || !strings.Contains(note, "create_dashboard")) {
				t.Fatalf("locked note = %q", note)
			}
		})
	}
}
