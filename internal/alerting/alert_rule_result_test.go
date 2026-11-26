package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type expectedAlertLink struct {
	URL   string
	Label string
}

func assertAlertLinkMeta(t *testing.T, result *mcp.CallToolResult, want expectedAlertLink) {
	t.Helper()
	raw, err := json.Marshal(result.Meta)
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		ReferenceURL string `json:"reference_url"`
		Links        []struct {
			Label string `json:"label"`
			URL   string `json:"url"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.ReferenceURL != want.URL {
		t.Errorf("reference_url = %q, want %q", meta.ReferenceURL, want.URL)
	}
	if len(meta.Links) != 1 {
		t.Fatalf("links = %s, want one labeled link when group name is unknown", raw)
	}
	if meta.Links[0].Label != want.Label || meta.Links[0].URL != want.URL {
		t.Errorf("links = %s", raw)
	}
}

func TestCreateAlertRuleLinkEscapesIdentifiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"rule/+?&=#雪"}`))
	}))
	defer server.Close()
	cfg := testAlertMutationConfig(server.URL)
	cfg.OrgSlug = "acme"
	result, _, err := NewCreateAlertHandler(server.Client(), cfg)(context.Background(), nil, CreateAlertArgs{
		EntityID: "group/+?&=#雪", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAlertLinkMeta(t, result, expectedAlertLink{URL: "/v2/organizations/acme/alerting/groups?group_id=group%2F%2B%3F%26%3D%23%E9%9B%AA&rule=rule%2F%2B%3F%26%3D%23%E9%9B%AA", Label: "Open alert"})
}
