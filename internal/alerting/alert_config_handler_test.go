package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type alertConfigTestServerState struct {
	alertRules         AlertConfigResponse
	entityGroups       []groupedAlertGroupEntitiesResponse
	alertRulesStatus   int
	entityLookupStatus int
	entityLookupCalls  int
	lastEntityRequest  filterAlertGroupEntitiesRequest
}

func TestGetAlertConfigHandler_RuleOnlyFilters(t *testing.T) {
	tests := []struct {
		name        string
		args        GetAlertConfigArgs
		expectedIDs []string
	}{
		{
			name:        "no filters",
			args:        GetAlertConfigArgs{},
			expectedIDs: []string{"rule-1", "rule-2", "rule-3"},
		},
		{
			name: "severity filter",
			args: GetAlertConfigArgs{
				Severity: "BREACH",
			},
			expectedIDs: []string{"rule-1", "rule-3"},
		},
		{
			name: "rule type filter",
			args: GetAlertConfigArgs{
				RuleType: "static",
			},
			expectedIDs: []string{"rule-1"},
		},
		{
			name: "algorithm filter",
			args: GetAlertConfigArgs{
				Algorithm: "HIGH_SPIKE",
			},
			expectedIDs: []string{"rule-2"},
		},
		{
			name: "state filter",
			args: GetAlertConfigArgs{
				State: "disabled",
			},
			expectedIDs: []string{"rule-3"},
		},
		{
			name: "rule name substring filter",
			args: GetAlertConfigArgs{
				RuleName: "latency",
			},
			expectedIDs: []string{"rule-1"},
		},
		{
			name: "entity ids filter",
			args: GetAlertConfigArgs{
				EntityIDs: []string{"entity-2", "entity-3"},
			},
			expectedIDs: []string{"rule-2", "rule-3"},
		},
		{
			name: "external ref substring filter",
			args: GetAlertConfigArgs{
				ExternalRef: "errors",
			},
			expectedIDs: []string{"rule-2"},
		},
		{
			name: "mismatched rule type and algorithm",
			args: GetAlertConfigArgs{
				RuleType:  "static",
				Algorithm: "high_spike",
			},
			expectedIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := alertConfigTestServerState{
				alertRules:         sampleAlertConfigRules(),
				entityGroups:       sampleAlertGroupEntities(),
				alertRulesStatus:   http.StatusOK,
				entityLookupStatus: http.StatusOK,
			}

			text, _, err := executeGetAlertConfig(t, &state, tt.args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			assertAlertConfigResultIDs(t, text, tt.expectedIDs)
			if state.entityLookupCalls != 0 {
				t.Fatalf("expected no entity lookup for rule-only filters, got %d call(s)", state.entityLookupCalls)
			}
		})
	}
}

func TestGetAlertConfigHandler_EntityBackedFiltersAndSearch(t *testing.T) {
	tests := []struct {
		name                    string
		args                    GetAlertConfigArgs
		expectedIDs             []string
		assertEntityLookupCalls int
		assertEntityRequest     func(*testing.T, filterAlertGroupEntitiesRequest)
	}{
		{
			name: "alert group name filter",
			args: GetAlertConfigArgs{
				AlertGroupName: "payments",
			},
			expectedIDs:             []string{"rule-2"},
			assertEntityLookupCalls: 1,
		},
		{
			name: "alert group type filter",
			args: GetAlertConfigArgs{
				AlertGroupType: "scheduled",
			},
			expectedIDs:             []string{"rule-2"},
			assertEntityLookupCalls: 1,
		},
		{
			name: "data source name filter",
			args: GetAlertConfigArgs{
				DataSourceName: "grafana",
			},
			expectedIDs:             []string{"rule-1"},
			assertEntityLookupCalls: 1,
		},
		{
			name: "tags filter",
			args: GetAlertConfigArgs{
				Tags: []string{"prod", "check"},
			},
			expectedIDs:             []string{"rule-1"},
			assertEntityLookupCalls: 1,
			assertEntityRequest: func(t *testing.T, req filterAlertGroupEntitiesRequest) {
				expected := []alertGroupEntityFilter{
					newAlertGroupEntityFilter(entityFilterEntityClass, alertGroupEntityClassGrafanaAlerts, entityFilterEqual),
					newAlertGroupEntityFilter(entityFilterTags, "prod", entityFilterContains),
					newAlertGroupEntityFilter(entityFilterTags, "check", entityFilterContains),
					{
						FilterType:  entityFilterEntityClass,
						FilterKey:   alertGroupEntityClassAlertManager,
						FilterValue: alertGroupEntityClassAlertManager,
						Operator:    entityFilterEqual,
						Conjunction: "or",
					},
					newAlertGroupEntityFilter(entityFilterTags, "prod", entityFilterContains),
					newAlertGroupEntityFilter(entityFilterTags, "check", entityFilterContains),
				}

				if len(req.Filters) != len(expected) {
					t.Fatalf("unexpected entity filter count: got %d want %d", len(req.Filters), len(expected))
				}

				for i := range expected {
					if req.Filters[i] != expected[i] {
						t.Fatalf("unexpected entity filter at index %d: got %#v want %#v", i, req.Filters[i], expected[i])
					}
				}
			},
		},
		{
			name: "search term matches rule fields",
			args: GetAlertConfigArgs{
				SearchTerm: "latency",
			},
			expectedIDs:             []string{"rule-1"},
			assertEntityLookupCalls: 1,
		},
		{
			name: "search term matches entity fields",
			args: GetAlertConfigArgs{
				SearchTerm: "payments",
			},
			expectedIDs:             []string{"rule-2"},
			assertEntityLookupCalls: 1,
		},
		{
			name: "typed filters and search term combine with AND",
			args: GetAlertConfigArgs{
				Severity:   "breach",
				SearchTerm: "inventory",
			},
			expectedIDs:             []string{"rule-3"},
			assertEntityLookupCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := alertConfigTestServerState{
				alertRules:         sampleAlertConfigRules(),
				entityGroups:       sampleAlertGroupEntities(),
				alertRulesStatus:   http.StatusOK,
				entityLookupStatus: http.StatusOK,
			}

			text, _, err := executeGetAlertConfig(t, &state, tt.args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			assertAlertConfigResultIDs(t, text, tt.expectedIDs)
			if state.entityLookupCalls != tt.assertEntityLookupCalls {
				t.Fatalf("unexpected entity lookup calls: got %d want %d", state.entityLookupCalls, tt.assertEntityLookupCalls)
			}
			if tt.assertEntityRequest != nil {
				tt.assertEntityRequest(t, state.lastEntityRequest)
			}
		})
	}
}

func TestGetAlertConfigHandler_EntityLookupFailure(t *testing.T) {
	tests := []struct {
		name string
		args GetAlertConfigArgs
	}{
		{
			name: "typed entity-backed filter",
			args: GetAlertConfigArgs{
				Tags: []string{"prod"},
			},
		},
		{
			name: "search term",
			args: GetAlertConfigArgs{
				SearchTerm: "latency",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := alertConfigTestServerState{
				alertRules:         sampleAlertConfigRules(),
				entityGroups:       sampleAlertGroupEntities(),
				alertRulesStatus:   http.StatusOK,
				entityLookupStatus: http.StatusInternalServerError,
			}

			_, _, err := executeGetAlertConfig(t, &state, tt.args)
			if err == nil {
				t.Fatal("expected handler to fail when entity lookup fails")
			}
			if !strings.Contains(err.Error(), "failed to fetch alert group entities") {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.entityLookupCalls != 1 {
				t.Fatalf("expected one entity lookup call, got %d", state.entityLookupCalls)
			}
		})
	}
}

func TestGetAlertConfigHandler_InvalidRuleType(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:         sampleAlertConfigRules(),
		entityGroups:       sampleAlertGroupEntities(),
		alertRulesStatus:   http.StatusOK,
		entityLookupStatus: http.StatusOK,
	}

	_, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{RuleType: "unknown"})
	if err == nil {
		t.Fatal("expected invalid rule type to return an error")
	}
	if !strings.Contains(err.Error(), "rule_type must be one of") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func executeGetAlertConfig(
	t *testing.T,
	state *alertConfigTestServerState,
	args GetAlertConfigArgs,
) (string, *mcp.CallToolResult, error) {
	t.Helper()

	server := newAlertConfigTestServer(t, state)
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-access-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewGetAlertConfigHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", result, err
	}

	return utils.GetTextContent(t, result), result, nil
}

func newAlertConfigTestServer(
	t *testing.T,
	state *alertConfigTestServerState,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointAlertRules:
			w.Header().Set("Content-Type", "application/json")
			status := state.alertRulesStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if status == http.StatusOK {
				_ = json.NewEncoder(w).Encode(state.alertRules)
				return
			}
			_, _ = w.Write([]byte(`{"error":"alert rules failed"}`))
		case constants.EndpointEntitiesList:
			state.entityLookupCalls++
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&state.lastEntityRequest)

			w.Header().Set("Content-Type", "application/json")
			status := state.entityLookupStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if status == http.StatusOK {
				_ = json.NewEncoder(w).Encode(state.entityGroups)
				return
			}
			_, _ = w.Write([]byte(`{"error":"entity lookup failed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func sampleAlertConfigRules() AlertConfigResponse {
	return AlertConfigResponse{
		{
			ID:                           "rule-1",
			OrganizationID:               "org-1",
			EntityID:                     "entity-1",
			PrimaryIndicator:             "latency_ms",
			CreatedAt:                    1700000000,
			UpdatedAt:                    1700000600,
			State:                        "active",
			ExternalRef:                  "checkout-latency",
			Severity:                     "breach",
			Algorithm:                    "static_threshold",
			RuleName:                     "High latency",
			MuteUntil:                    0,
			Properties:                   map[string]interface{}{"description": "High latency alert"},
			GroupTimeseriesNotifications: true,
		},
		{
			ID:                           "rule-2",
			OrganizationID:               "org-1",
			EntityID:                     "entity-2",
			PrimaryIndicator:             "error_rate",
			CreatedAt:                    1700001200,
			UpdatedAt:                    1700001800,
			State:                        "muted",
			ExternalRef:                  "payments-errors",
			Severity:                     "threat",
			Algorithm:                    "high_spike",
			RuleName:                     "Error spike",
			MuteUntil:                    1700003600,
			Properties:                   map[string]interface{}{"description": "Payments spike"},
			GroupTimeseriesNotifications: false,
		},
		{
			ID:                           "rule-3",
			OrganizationID:               "org-1",
			EntityID:                     "entity-3",
			PrimaryIndicator:             "cpu_usage",
			CreatedAt:                    1700002400,
			UpdatedAt:                    1700003000,
			State:                        "disabled",
			ExternalRef:                  "inventory-cpu",
			Severity:                     "breach",
			Algorithm:                    "inc_trend",
			RuleName:                     "CPU trend",
			MuteUntil:                    0,
			Properties:                   map[string]interface{}{"description": "Inventory CPU trend"},
			GroupTimeseriesNotifications: true,
		},
	}
}

func sampleAlertGroupEntities() []groupedAlertGroupEntitiesResponse {
	return []groupedAlertGroupEntitiesResponse{
		{
			Entities: []alertGroupEntity{
				{
					ID:             "entity-1",
					Name:           "Checkout Alerts",
					Type:           "grafana-dashboard",
					DataSourceName: "Grafana Prod",
					Metadata: alertGroupEntityMetadata{
						Tags: []string{"prod", "checkout"},
					},
				},
				{
					ID:             "entity-2",
					Name:           "Payments Monitor",
					Type:           "scheduled-search",
					DataSourceName: "Loki Payments",
					Metadata: alertGroupEntityMetadata{
						Tags: []string{"payments", "staging"},
					},
				},
				{
					ID:             "entity-3",
					Name:           "Inventory Group",
					Type:           "alert-group",
					DataSourceName: "Prometheus Inventory",
					Metadata: alertGroupEntityMetadata{
						Tags: []string{"inventory", "prod-east"},
					},
				},
			},
		},
	}
}

func assertAlertConfigResultIDs(t *testing.T, text string, expectedIDs []string) {
	t.Helper()

	expectedCountLine := fmt.Sprintf("Found %d alert rules:", len(expectedIDs))
	if !strings.Contains(text, expectedCountLine) {
		t.Fatalf("response did not contain count line %q:\n%s", expectedCountLine, text)
	}

	for _, id := range []string{"rule-1", "rule-2", "rule-3"} {
		expected := false
		for _, expectedID := range expectedIDs {
			if id == expectedID {
				expected = true
				break
			}
		}

		containsID := strings.Contains(text, fmt.Sprintf("ID: %s", id))
		if expected && !containsID {
			t.Fatalf("expected response to contain %s:\n%s", id, text)
		}
		if !expected && containsID {
			t.Fatalf("expected response to exclude %s:\n%s", id, text)
		}
	}
}
