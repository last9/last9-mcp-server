package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAlertWritesReferencePersistedRule(t *testing.T) {
	const groupID = "group /&+?"
	const savedID = "saved /&+?"
	const oldID = "old-rule"
	for _, tool := range []string{"create", "update", "patch", "chart"} {
		for _, groupName := range []string{"", "Payments & API / 東京 + prod"} {
			t.Run(tool+"/"+groupName, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						_ = json.NewEncoder(w).Encode(map[string]string{"id": oldID, "entity_id": groupID, "group_name": groupName})
						return
					}
					if r.Method == http.MethodPatch {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"id": savedID, "group_id": groupID, "group_name": groupName})
				}))
				defer server.Close()
				cfg := createAlertFromChartTestConfig(server.URL)
				result, err := callAlertLinkWrite(server.Client(), cfg, alertLinkWrite{tool, groupID})
				if err != nil || result == nil || result.IsError {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				wantID := savedID
				if tool == "patch" {
					wantID = oldID
				}
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				var wire struct {
					Meta struct {
						ReferenceURL string `json:"reference_url"`
						Links        []struct {
							Label string `json:"label"`
							URL   string `json:"url"`
						} `json:"links"`
					} `json:"_meta"`
				}
				if err := json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				link, err := url.Parse(wire.Meta.ReferenceURL)
				if err != nil {
					t.Fatal(err)
				}
				if link.IsAbs() || link.Host != "" || link.Path != "/v2/organizations/acme/alerting/groups" || link.Query().Get("rule") != wantID || link.Query().Get("group_id") != groupID || len(link.Query()) != 2 {
					t.Errorf("reference_url = %q, want persisted rule %q in group %q", link, wantID, groupID)
				}
				wantLinks := 1
				if groupName != "" {
					wantLinks++
				}
				if len(wire.Meta.Links) != wantLinks {
					t.Fatalf("want %d links: %s", wantLinks, raw)
				}
				if wire.Meta.Links[0].Label != "Open alert" || wire.Meta.Links[0].URL != wire.Meta.ReferenceURL {
					t.Errorf("incorrect alert link: %+v", wire.Meta.Links[0])
				}
				if groupName == "" {
					return
				}
				groupLink, err := url.Parse(wire.Meta.Links[1].URL)
				if err != nil {
					t.Fatal(err)
				}
				if wire.Meta.Links[1].Label != "Open group" || groupLink.IsAbs() || groupLink.Host != "" || groupLink.Path != link.Path || groupLink.Query().Get("alertGroupNames") != groupName || len(groupLink.Query()) != 1 {
					t.Errorf("incorrect group link: %+v", wire.Meta.Links[1])
				}
			})
		}
	}
}

func TestFailedAlertWritesOmitResourceLinks(t *testing.T) {
	for _, tool := range []string{"create", "update", "patch", "chart"} {
		t.Run(tool, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveRuleOwner(w, r) {
					return
				}
				http.Error(w, "write refused", http.StatusForbidden)
			}))
			defer server.Close()
			result, err := callAlertLinkWrite(server.Client(), createAlertFromChartTestConfig(server.URL), alertLinkWrite{tool, "group-1"})
			if err == nil && (result == nil || !result.IsError) {
				t.Fatalf("want failed write: result=%+v", result)
			}
			if result != nil {
				for _, key := range []string{"reference_url", "links"} {
					if _, ok := result.Meta[key]; ok {
						t.Errorf("failed write carries %s", key)
					}
				}
			}
		})
	}
}

type alertLinkWrite struct{ tool, groupID string }

func callAlertLinkWrite(client *http.Client, cfg models.Config, write alertLinkWrite) (*mcp.CallToolResult, error) {
	ctx := context.Background()
	var result *mcp.CallToolResult
	var err error
	switch write.tool {
	case "create":
		result, _, err = NewCreateAlertHandler(client, cfg)(ctx, nil, CreateAlertArgs{EntityID: write.groupID, AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	case "update":
		result, _, err = NewUpdateAlertHandler(client, cfg)(ctx, nil, UpdateAlertArgs{EntityID: write.groupID, ID: "old-rule", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	case "patch":
		disabled := true
		result, _, err = NewPatchAlertHandler(client, cfg)(ctx, nil, PatchAlertArgs{EntityID: write.groupID, ID: "old-rule", IsDisabled: &disabled})
	case "chart":
		args := checkoutChartArgs
		args.GroupID = "request-group"
		result, _, err = NewCreateAlertFromChartHandler(client, cfg)(ctx, nil, args)
	}
	return result, err
}

func TestUpdateAlertKeepsKnownGroupName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id":"old-rule","entity_id":"group-1","group_name":"Known group"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"replacement-rule"}`))
	}))
	defer server.Close()
	result, err := callAlertLinkWrite(server.Client(), createAlertFromChartTestConfig(server.URL), alertLinkWrite{"update", "group-1"})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	raw, err := json.Marshal(result.Meta["links"])
	if err != nil {
		t.Fatal(err)
	}
	var links []map[string]string
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || links[1]["url"] != "/v2/organizations/acme/alerting/groups?alertGroupNames=Known+group" {
		t.Fatalf("lost known group name: %s", raw)
	}
}
