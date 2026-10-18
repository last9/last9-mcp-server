package alerting

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serveRuleOwner answers the ownership lookup patch/update/delete make before writing,
// reporting the rule as owned by the entity in the request path.
func serveRuleOwner(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(r.URL.EscapedPath(), "/")
	if r.Method != http.MethodGet || len(parts) != 5 || parts[1] != "entities" || parts[3] != "alert-rules" {
		return false
	}
	_, _ = w.Write([]byte(fmt.Sprintf(`{"id":%q,"entity_id":%q}`, parts[4], parts[2])))
	return true
}

// serveEmptyRuleList answers create_alert's duplicate-name lookup with a group that has no rules.
func serveEmptyRuleList(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(r.URL.EscapedPath(), "/")
	if r.Method != http.MethodGet || len(parts) != 4 || parts[1] != "entities" || parts[3] != "alert-rules" {
		return false
	}
	_, _ = w.Write([]byte(`{"rules":[]}`))
	return true
}

func newAlertRuleServer(t *testing.T, method, path, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRuleOwner(w, r) || serveEmptyRuleList(w, r) {
			return
		}
		if r.Method != method {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != path {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		if got := r.Header.Get("X-LAST9-API-TOKEN"); got == "" {
			t.Error("missing API token")
		}
		got, _ := io.ReadAll(r.Body)
		if string(got) != body {
			t.Errorf("body = %s", got)
		}
		if method == http.MethodPost || method == http.MethodPut {
			_, _ = w.Write([]byte(`{"id":"rule-1"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func resultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil {
		return ""
	}
	return text.Text
}

func testAlertMutationConfig(baseURL string) models.Config {
	return models.Config{APIBaseURL: baseURL, TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)}}
}

func validAlertRulePayload(name *string) AlertRulePayload {
	return AlertRulePayload{
		PrimaryIndicator: "latency",
		ExpressionArgs:   map[string]AlertExpressionArg{"latency": {ID: "kpi-1"}},
		RuleName:         name,
	}
}
