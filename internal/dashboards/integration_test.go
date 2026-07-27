package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const integrationTestTimeout = 30 * time.Second

func minimalDashboardPayload(name string) (dashboard, metadata json.RawMessage) {
	dashboard = json.RawMessage(fmt.Sprintf(`{
		"name": %q,
		"panels": [{
			"name": "panel-a",
			"version": 1,
			"layout": {"x": 0, "y": 0, "w": 6, "h": 6},
			"visualization": {"type": "stat"},
			"queries": [{
				"name": "A",
				"type": "range",
				"expr": "1",
				"telemetry": "metrics",
				"query_type": "promql",
				"legend": {"type": "auto", "value": ""}
			}]
		}]
	}`, name))
	metadata = json.RawMessage(`{"_category":"custom","_type":"metrics"}`)
	return dashboard, metadata
}

func TestListDashboardsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewListDashboardsHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()
	result, _, err := handler(ctx, &mcp.CallToolRequest{}, ListDashboardsArgs{})
	if utils.CheckAPIError(t, err) {
		return
	}

	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL == "" {
		t.Fatalf("expected reference_url in meta, got %v", result.Meta)
	}
	if !strings.Contains(refURL, "/dashboards") {
		t.Fatalf("unexpected reference_url %q", refURL)
	}

	text := utils.GetTextContent(t, result)
	if !json.Valid([]byte(text)) {
		t.Fatalf("expected JSON body, got: %s", text)
	}
	t.Logf("list_dashboards ok (reference_url=%s, bytes=%d)", refURL, len(text))
}

func TestGetDashboardHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	client := http.DefaultClient

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()
	listResult, _, err := NewListDashboardsHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, ListDashboardsArgs{})
	if utils.CheckAPIError(t, err) {
		return
	}

	listText := utils.GetTextContent(t, listResult)
	var overviews []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(listText), &overviews); err != nil {
		t.Fatalf("decode list response: %v\nbody: %s", err, listText)
	}
	if len(overviews) == 0 {
		t.Skip("no dashboards in org; skipping get_dashboard integration test")
	}

	dashboardID := overviews[0].ID
	if dashboardID == "" {
		t.Fatal("first dashboard has empty id")
	}

	getHandler := NewGetDashboardHandler(client, *cfg)
	result, _, err := getHandler(ctx, &mcp.CallToolRequest{}, GetDashboardArgs{ID: dashboardID})
	if utils.CheckAPIError(t, err) {
		return
	}

	wantRef := "/v2/organizations/" + cfg.OrgSlug + "/dashboards/" + dashboardID
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != wantRef {
		t.Fatalf("reference_url %q want %q", refURL, wantRef)
	}

	text := utils.GetTextContent(t, result)
	var envelope struct {
		Dashboard struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode get response: %v\nbody: %s", err, text)
	}
	if envelope.Dashboard.ID != dashboardID {
		t.Fatalf("dashboard id %q want %q", envelope.Dashboard.ID, dashboardID)
	}
	t.Logf("get_dashboard ok id=%s name=%q region=%s", dashboardID, envelope.Dashboard.Name, cfg.Region)
}

func TestDashboardCRUD_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	deleteCfg := utils.SetupTestConfigWithTokenOrSkip(t, "LAST9_DELETE_TOKEN", cfg)
	client := http.DefaultClient
	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	suffix := time.Now().UnixNano()
	createName := fmt.Sprintf("mcp-e2e-%d", suffix)
	updateName := createName + "-updated"

	createDash, meta := minimalDashboardPayload(createName)
	createResult, _, err := NewCreateDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, CreateDashboardArgs{
		DashboardRequest: DashboardRequest{Dashboard: createDash, Metadata: meta},
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	createText := utils.GetTextContent(t, createResult)
	dashboardID := dashboardIDFromResponse([]byte(createText))
	if dashboardID == "" {
		t.Fatalf("create response missing dashboard.id: %s", createText)
	}

	wantCreateRef := "/v2/organizations/" + cfg.OrgSlug + "/dashboards/" + dashboardID
	if createRef, ok := createResult.Meta["reference_url"].(string); !ok || createRef != wantCreateRef {
		t.Fatalf("create reference_url %q want %q", createResult.Meta["reference_url"], wantCreateRef)
	}

	t.Cleanup(func() {
		_, _, _ = NewDeleteDashboardHandler(client, *deleteCfg)(ctx, &mcp.CallToolRequest{}, DeleteDashboardArgs{ID: dashboardID})
	})

	updateDash, _ := minimalDashboardPayload(updateName)
	_, _, err = NewUpdateDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, UpdateDashboardArgs{
		ID: dashboardID,
		DashboardRequest: DashboardRequest{
			Dashboard: updateDash,
			Metadata:  meta,
		},
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	getResult, _, err := NewGetDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, GetDashboardArgs{ID: dashboardID})
	if utils.CheckAPIError(t, err) {
		return
	}
	var got struct {
		Dashboard struct {
			Name string `json:"name"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, getResult)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Dashboard.Name != updateName {
		t.Fatalf("name %q want %q", got.Dashboard.Name, updateName)
	}

	deleteResult, _, err := NewDeleteDashboardHandler(client, *deleteCfg)(ctx, &mcp.CallToolRequest{}, DeleteDashboardArgs{ID: dashboardID})
	if utils.CheckAPIError(t, err) {
		return
	}

	wantDeleteRef := "/v2/organizations/" + cfg.OrgSlug + "/dashboards"
	if deleteRef, ok := deleteResult.Meta["reference_url"].(string); !ok || deleteRef != wantDeleteRef {
		t.Fatalf("delete reference_url %q want %q", deleteResult.Meta["reference_url"], wantDeleteRef)
	}

	_, _, err = NewGetDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, GetDashboardArgs{ID: dashboardID})
	if err == nil {
		t.Fatal("expected error fetching deleted dashboard")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected not found after delete, got: %v", err)
	}
	t.Logf("dashboard CRUD ok id=%s", dashboardID)
}

func TestDashboardSnapshotCRUD_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	deleteCfg := utils.SetupTestConfigWithTokenOrSkip(t, "LAST9_DELETE_TOKEN", cfg)
	client := http.DefaultClient
	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	suffix := time.Now().UnixNano()
	dashName := fmt.Sprintf("mcp-snap-e2e-%d", suffix)
	createDash, meta := minimalDashboardPayload(dashName)
	createResult, _, err := NewCreateDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, CreateDashboardArgs{
		DashboardRequest: DashboardRequest{Dashboard: createDash, Metadata: meta},
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	dashboardID := dashboardIDFromResponse([]byte(utils.GetTextContent(t, createResult)))
	if dashboardID == "" {
		t.Fatalf("create response missing dashboard.id: %s", utils.GetTextContent(t, createResult))
	}
	t.Cleanup(func() {
		_, _, _ = NewDeleteDashboardHandler(client, *deleteCfg)(context.Background(), &mcp.CallToolRequest{}, DeleteDashboardArgs{ID: dashboardID})
	})

	getResult, _, err := NewGetDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, GetDashboardArgs{ID: dashboardID})
	if utils.CheckAPIError(t, err) {
		return
	}
	var getEnvelope struct {
		Dashboard json.RawMessage `json:"dashboard"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, getResult)), &getEnvelope); err != nil {
		t.Fatalf("decode get dashboard: %v", err)
	}
	if len(getEnvelope.Dashboard) == 0 {
		t.Fatal("get dashboard returned empty dashboard object")
	}

	now := time.Now().Unix()
	expires := now + 3600
	snapName := fmt.Sprintf("mcp-snapshot-%d", suffix)
	// create_dashboard_snapshot is not exposed as an MCP tool (server-side capture
	// pending — ENG-1492). Seed a snapshot directly via the v4 API so the shipped
	// list/get/delete tools still get real end-to-end coverage.
	seedBody, err := json.Marshal(map[string]any{
		"dashboard_id":         dashboardID,
		"name":                 snapName,
		"description":          "mcp e2e snapshot",
		"expires_at":           expires,
		"time_range":           json.RawMessage(fmt.Sprintf(`{"from":%d,"to":%d}`, now-3600, now)),
		"variables":            json.RawMessage(`{}`),
		"region":               cfg.Region,
		"dashboard_definition": getEnvelope.Dashboard,
		"panel_data":           json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("marshal seed snapshot: %v", err)
	}
	seedResp, _, err := doJSONRequest(ctx, client, *cfg, http.MethodPost, cfg.APIBaseURL+constants.EndpointDashboardSnapshots, seedBody)
	if utils.CheckAPIError(t, err) {
		return
	}
	snapshotID := snapshotIDFromResponse(seedResp)
	if snapshotID == "" {
		t.Fatalf("seed snapshot missing id: %s", string(seedResp))
	}
	t.Cleanup(func() {
		_, _, _ = NewDeleteDashboardSnapshotHandler(client, *deleteCfg)(context.Background(), &mcp.CallToolRequest{}, DeleteDashboardSnapshotArgs{ID: snapshotID})
	})

	listResult, _, err := NewListDashboardSnapshotsHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, ListDashboardSnapshotsArgs{
		DashboardID: dashboardID,
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	listText := utils.GetTextContent(t, listResult)
	if !strings.Contains(listText, snapshotID) || !strings.Contains(listText, snapName) {
		t.Fatalf("list snapshots missing created snapshot: %s", listText)
	}

	getSnapResult, _, err := NewGetDashboardSnapshotHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, GetDashboardSnapshotArgs{ID: snapshotID})
	if utils.CheckAPIError(t, err) {
		return
	}
	var snap struct {
		ID                  string          `json:"id"`
		Name                string          `json:"name"`
		DashboardDefinition json.RawMessage `json:"dashboard_definition"`
		PanelData           json.RawMessage `json:"panel_data"`
		TimeRange           json.RawMessage `json:"time_range"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, getSnapResult)), &snap); err != nil {
		t.Fatalf("decode get snapshot: %v", err)
	}
	if snap.ID != snapshotID || snap.Name != snapName {
		t.Fatalf("get snapshot id/name got %q/%q want %q/%q", snap.ID, snap.Name, snapshotID, snapName)
	}
	if len(snap.DashboardDefinition) == 0 || len(snap.PanelData) == 0 || len(snap.TimeRange) == 0 {
		t.Fatalf("get snapshot missing frozen fields: %+v", snap)
	}
	wantSnapRef := "/v2/organizations/" + cfg.OrgSlug + "/dashboards/snapshots/" + snapshotID
	if ref, ok := getSnapResult.Meta["reference_url"].(string); !ok || ref != wantSnapRef {
		t.Fatalf("get snapshot reference_url %q want %q", getSnapResult.Meta["reference_url"], wantSnapRef)
	}

	deleteResult, _, err := NewDeleteDashboardSnapshotHandler(client, *deleteCfg)(ctx, &mcp.CallToolRequest{}, DeleteDashboardSnapshotArgs{ID: snapshotID})
	if utils.CheckAPIError(t, err) {
		return
	}
	if !strings.Contains(utils.GetTextContent(t, deleteResult), snapshotID) {
		t.Fatalf("delete response missing id: %s", utils.GetTextContent(t, deleteResult))
	}

	_, _, err = NewGetDashboardSnapshotHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, GetDashboardSnapshotArgs{ID: snapshotID})
	if err == nil {
		t.Fatal("expected error fetching deleted snapshot")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found after delete, got: %v", err)
	}
	t.Logf("dashboard snapshot CRUD ok dashboard=%s snapshot=%s", dashboardID, snapshotID)
}
