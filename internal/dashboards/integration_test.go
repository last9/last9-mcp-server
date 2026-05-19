package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardsArgs{})
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

	listResult, _, err := NewListDashboardsHandler(client, *cfg)(context.Background(), &mcp.CallToolRequest{}, ListDashboardsArgs{})
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
	result, _, err := getHandler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: dashboardID})
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
	client := http.DefaultClient
	ctx := context.Background()

	suffix := time.Now().Unix()
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
		_, _, _ = NewDeleteDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, DeleteDashboardArgs{ID: dashboardID})
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

	deleteResult, _, err := NewDeleteDashboardHandler(client, *cfg)(ctx, &mcp.CallToolRequest{}, DeleteDashboardArgs{ID: dashboardID})
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
