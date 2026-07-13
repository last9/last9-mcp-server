package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateCreateSnapshotArgs_TimeRangeAndExpiresAt(t *testing.T) {
	base := CreateDashboardSnapshotArgs{
		DashboardID:         "dash-1",
		Name:                "Freeze",
		TimeRange:           json.RawMessage(`{"from":100,"to":200}`),
		DashboardDefinition: json.RawMessage(`{"name":"Dash"}`),
		PanelData:           json.RawMessage(`{"p1":[]}`),
	}

	if err := validateCreateSnapshotArgs(base); err != nil {
		t.Fatalf("valid args: %v", err)
	}

	badRange := base
	badRange.TimeRange = json.RawMessage(`{"from":200,"to":100}`)
	if err := validateCreateSnapshotArgs(badRange); err == nil || !strings.Contains(err.Error(), "from must be less") {
		t.Fatalf("inverted range: %v", err)
	}

	missingTo := base
	missingTo.TimeRange = json.RawMessage(`{"from":100}`)
	if err := validateCreateSnapshotArgs(missingTo); err == nil || !strings.Contains(err.Error(), "time_range") {
		t.Fatalf("missing to: %v", err)
	}

	past := time.Now().Add(-time.Hour).Unix()
	expired := base
	expired.ExpiresAt = &past
	if err := validateCreateSnapshotArgs(expired); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("past expires_at: %v", err)
	}

	ms := time.Now().UnixMilli() + 60_000
	millis := base
	millis.ExpiresAt = &ms
	if err := validateCreateSnapshotArgs(millis); err == nil || !strings.Contains(err.Error(), "milliseconds") {
		t.Fatalf("ms expires_at: %v", err)
	}
}

func TestNormalizeCreateSnapshotArgs_DefaultsVariables(t *testing.T) {
	args := CreateDashboardSnapshotArgs{}
	normalizeCreateSnapshotArgs(&args)
	if string(args.Variables) != "{}" {
		t.Fatalf("variables %q", args.Variables)
	}
	args.Variables = json.RawMessage(`{"env":"prod"}`)
	normalizeCreateSnapshotArgs(&args)
	if string(args.Variables) != `{"env":"prod"}` {
		t.Fatalf("variables mutated %q", args.Variables)
	}
}

func TestCreateDashboardSnapshotHandler_DefaultsMissingVariables(t *testing.T) {
	var captured map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"id":"snap-1","name":"Freeze"}`))
	}))
	defer srv.Close()

	handler := NewCreateDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardSnapshotArgs{
		DashboardID:         "dash-1",
		Name:                "Freeze",
		TimeRange:           json.RawMessage(`{"from":1,"to":2}`),
		DashboardDefinition: json.RawMessage(`{"name":"Dash"}`),
		PanelData:           json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(captured["variables"]) != "{}" {
		t.Fatalf("variables %s", captured["variables"])
	}
}
