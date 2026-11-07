package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestConcurrentPatchDashboardKeepsBothChanges fakes a store whose reads wait for each
// other, so two unserialized patches both read the same version before either writes.
func TestConcurrentPatchDashboardKeepsBothChanges(t *testing.T) {
	var mu sync.Mutex
	stored := patchFixture
	barrier := utils.NewReadBarrier(2, 300*time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			barrier.Wait()
			mu.Lock()
			defer mu.Unlock()
			_, _ = io.WriteString(w, stored)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			defer mu.Unlock()
			stored = string(body)
			_, _ = io.WriteString(w, `{"dashboard":{"id":"dash-1"}}`)
		}
	}))
	t.Cleanup(srv.Close)

	handler := NewPatchDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	patch := func(ops string) error {
		var args PatchDashboardArgs
		if err := json.Unmarshal([]byte(`{"id":"dash-1","ops":`+ops+`}`), &args); err != nil {
			return err
		}
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
		if err == nil && result.IsError {
			t.Errorf("patch %s: %v", ops, result.Content)
		}
		return err
	}
	var wg sync.WaitGroup
	for _, ops := range []string{
		`[{"op":"add_panel","panel":{"id":"pA","name":"A","visualization":{"type":"timeseries"},"queries":[{"name":"A","expr":"up"}]}}]`,
		`[{"op":"set_title","title":"Renamed"}]`,
	} {
		wg.Go(func() {
			if err := patch(ops); err != nil {
				t.Errorf("patch %s: %v", ops, err)
			}
		})
	}
	wg.Wait()

	var got struct {
		Dashboard map[string]any `json:"dashboard"`
	}
	mu.Lock()
	defer mu.Unlock()
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatal(err)
	}
	if got.Dashboard["name"] != "Renamed" || !slices.Contains(panelIDs(got.Dashboard), "pA") {
		t.Fatalf("stored name=%v panels=%v, want both changes", got.Dashboard["name"], panelIDs(got.Dashboard))
	}
}
