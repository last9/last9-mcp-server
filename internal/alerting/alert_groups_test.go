package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetAlertGroupsHandler_InventoryAndCounts(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:         sampleAlertConfigRules(),
		entityGroups:       sampleAlertGroupsWithZeroRuleEntity(),
		alertRulesStatus:   http.StatusOK,
		entityLookupStatus: http.StatusOK,
	}

	body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	resp := decodeAlertGroupsResponse(t, body)
	if resp.Count != 4 {
		t.Fatalf("expected 4 groups including zero-rule entity, got %d: %s", resp.Count, body)
	}
	if state.entityLookupCalls != 1 {
		t.Fatalf("expected 1 entity lookup, got %d", state.entityLookupCalls)
	}

	byID := alertGroupsByID(resp.Groups)
	assertAlertGroupCounts(t, byID["entity-1"], 1, 1, 0)
	assertAlertGroupCounts(t, byID["entity-2"], 1, 1, 0)
	assertAlertGroupCounts(t, byID["entity-3"], 1, 0, 1)
	assertAlertGroupCounts(t, byID["entity-4"], 0, 0, 0)

	checkout := byID["entity-1"]
	if checkout.Team != "checkout" || checkout.Tier != "p1" || checkout.EntityClass != alertGroupEntityClassGrafanaAlerts {
		t.Fatalf("unexpected checkout group: %+v", checkout)
	}
	if checkout.Metadata.Labels["domain"] != "checkout" || checkout.Metadata.Labels["env"] != "prod" {
		t.Fatalf("unexpected checkout labels: %+v", checkout.Metadata.Labels)
	}

	unlabeled := byID["entity-3"]
	if unlabeled.Team != "" || unlabeled.Tier != "" {
		t.Fatalf("unlabeled group should have empty team/tier, got %+v", unlabeled)
	}
	if unlabeled.Metadata.Labels == nil || len(unlabeled.Metadata.Labels) != 0 {
		t.Fatalf("unlabeled group should emit empty labels object, got %#v", unlabeled.Metadata.Labels)
	}

	if resp.Groups[0].Name != "Checkout Alerts" {
		t.Fatalf("groups should be sorted by name, first was %q", resp.Groups[0].Name)
	}
}

func TestGetAlertGroupsHandler_Filters(t *testing.T) {
	t.Run("team", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupsWithZeroRuleEntity(),
			alertRulesStatus:   http.StatusOK,
			entityLookupStatus: http.StatusOK,
		}
		body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{Team: "PAYMENTS"})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		resp := decodeAlertGroupsResponse(t, body)
		assertAlertGroupIDs(t, resp, []string{"entity-2"})
		assertHasEntityFilter(t, state.lastEntityRequest, entityFilterTeam, "PAYMENTS", "PAYMENTS", entityFilterContains)
	})

	t.Run("tier", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupsWithZeroRuleEntity(),
			alertRulesStatus:   http.StatusOK,
			entityLookupStatus: http.StatusOK,
		}
		body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{Tier: "p1"})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		resp := decodeAlertGroupsResponse(t, body)
		assertAlertGroupIDs(t, resp, []string{"entity-1", "entity-4"})
		assertHasEntityFilter(t, state.lastEntityRequest, entityFilterTier, "p1", "p1", entityFilterContains)
	})

	t.Run("label pair", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupsWithZeroRuleEntity(),
			alertRulesStatus:   http.StatusOK,
			entityLookupStatus: http.StatusOK,
		}
		body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{
			LabelKey:   "domain",
			LabelValue: "checkout",
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		resp := decodeAlertGroupsResponse(t, body)
		assertAlertGroupIDs(t, resp, []string{"entity-1"})
		assertHasEntityFilter(t, state.lastEntityRequest, entityFilterLabel, "domain", "checkout", entityFilterContains)
	})

	t.Run("team is exact after contains", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupsWithZeroRuleEntity(),
			alertRulesStatus:   http.StatusOK,
			entityLookupStatus: http.StatusOK,
		}
		body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{Team: "pay"})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		resp := decodeAlertGroupsResponse(t, body)
		if resp.Count != 0 {
			t.Fatalf("substring team=pay must not match team=payments, got %v", groupIDs(resp.Groups))
		}
	})

	t.Run("alert group name", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupsWithZeroRuleEntity(),
			alertRulesStatus:   http.StatusOK,
			entityLookupStatus: http.StatusOK,
		}
		body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{AlertGroupName: "payments"})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		assertAlertGroupIDs(t, decodeAlertGroupsResponse(t, body), []string{"entity-2"})
	})
}

func TestGetAlertGroupsHandler_UnpairedLabelArgs(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:         sampleAlertConfigRules(),
		entityGroups:       sampleAlertGroupEntities(),
		alertRulesStatus:   http.StatusOK,
		entityLookupStatus: http.StatusOK,
	}

	t.Run("label_key only", func(t *testing.T) {
		_, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{LabelKey: "domain"})
		if err == nil {
			t.Fatal("expected unpaired label_key to error")
		}
		if err.Error() != "label_key and label_value must be set together" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("label_value only", func(t *testing.T) {
		_, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{LabelValue: "checkout"})
		if err == nil {
			t.Fatal("expected unpaired label_value to error")
		}
		if err.Error() != "label_key and label_value must be set together" {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGetAlertGroupsHandler_UpstreamErrors(t *testing.T) {
	t.Run("entity lookup failed", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupEntities(),
			alertRulesStatus:   http.StatusOK,
			entityLookupStatus: http.StatusInternalServerError,
		}
		_, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{})
		if err == nil {
			t.Fatal("expected entity lookup failure")
		}
	})

	t.Run("alert rules failed", func(t *testing.T) {
		state := alertConfigTestServerState{
			alertRules:         sampleAlertConfigRules(),
			entityGroups:       sampleAlertGroupEntities(),
			alertRulesStatus:   http.StatusInternalServerError,
			entityLookupStatus: http.StatusOK,
		}
		_, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{})
		if err == nil {
			t.Fatal("expected alert rules failure")
		}
	})
}

func TestGetAlertGroupsHandler_EmptyInventory(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:         AlertConfigResponse{},
		entityGroups:       []groupedAlertGroupEntitiesResponse{{Entities: []alertGroupEntity{}}},
		alertRulesStatus:   http.StatusOK,
		entityLookupStatus: http.StatusOK,
	}
	body, _, err := executeGetAlertGroups(t, &state, GetAlertGroupsArgs{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	resp := decodeAlertGroupsResponse(t, body)
	if resp.Count != 0 || resp.Groups == nil || len(resp.Groups) != 0 {
		t.Fatalf("expected empty groups array, got %s", body)
	}
}

func executeGetAlertGroups(
	t *testing.T,
	state *alertConfigTestServerState,
	args GetAlertGroupsArgs,
) (string, *mcp.CallToolResult, error) {
	t.Helper()

	server := newAlertConfigTestServer(t, state)
	t.Cleanup(server.Close)

	cfg := models.Config{
		APIBaseURL: server.URL,
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-access-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewGetAlertGroupsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", result, err
	}

	return utils.GetTextContent(t, result), result, nil
}

func decodeAlertGroupsResponse(t *testing.T, body string) alertGroupsResponse {
	t.Helper()
	var resp alertGroupsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, body)
	}
	return resp
}

func alertGroupsByID(groups []alertGroupResult) map[string]alertGroupResult {
	out := make(map[string]alertGroupResult, len(groups))
	for _, group := range groups {
		out[group.ID] = group
	}
	return out
}

func assertAlertGroupCounts(t *testing.T, group alertGroupResult, total, enabled, disabled int) {
	t.Helper()
	if group.ID == "" {
		t.Fatal("missing group")
	}
	if group.RulesCount != total || group.EnabledRulesCount != enabled || group.DisabledRulesCount != disabled {
		t.Fatalf("%s counts: got %d/%d/%d want %d/%d/%d", group.ID, group.RulesCount, group.EnabledRulesCount, group.DisabledRulesCount, total, enabled, disabled)
	}
}

func assertAlertGroupIDs(t *testing.T, resp alertGroupsResponse, want []string) {
	t.Helper()
	if resp.Count != len(want) {
		t.Fatalf("count=%d want %d ids=%v", resp.Count, len(want), groupIDs(resp.Groups))
	}
	got := groupIDs(resp.Groups)
	if len(got) != len(want) {
		t.Fatalf("ids=%v want %v", got, want)
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	for _, id := range got {
		if _, ok := wantSet[id]; !ok {
			t.Fatalf("ids=%v want %v", got, want)
		}
	}
}

func groupIDs(groups []alertGroupResult) []string {
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	return ids
}

func assertHasEntityFilter(t *testing.T, req filterAlertGroupEntitiesRequest, filterType, key, value, operator string) {
	t.Helper()
	for _, filter := range req.Filters {
		if filter.FilterType == filterType && filter.FilterKey == key && filter.FilterValue == value && filter.Operator == operator {
			return
		}
	}
	t.Fatalf("missing filter type=%s key=%s value=%s operator=%s in %#v", filterType, key, value, operator, req.Filters)
}

func sampleAlertGroupsWithZeroRuleEntity() []groupedAlertGroupEntitiesResponse {
	groups := sampleAlertGroupEntities()
	groups[0].Entities = append(groups[0].Entities, alertGroupEntity{
		ID:             "entity-4",
		Name:           "Infra Host Fleet",
		Type:           "alert-group",
		EntityClass:    alertGroupEntityClassAlertManager,
		Tier:           "p1",
		DataSourceName: "Prometheus",
		Metadata: alertGroupEntityMetadata{
			Team:   "infra",
			Labels: map[string]string{"env": "prod"},
		},
	})
	return groups
}
