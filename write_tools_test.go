package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/writefault"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeToolFixture is what one write tool needs to reach its write request: valid
// arguments, and canned answers (by path) to the GETs it makes first.
type writeToolFixture struct {
	args  string
	reads map[string]string
}

const testDashboard = `{"name":"Checkout","panels":[{"name":"p","version":1,"layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"queries":[{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":{"type":"auto","value":""}}]}]}`

const testAlertRule = `{"primary_indicator":"latency","expression_args":{"latency":{"id":"kpi-1"}},"rule_name":"checkout latency"}`

var ruleOwnedByEntity = map[string]string{"/entities/entity-1/alert-rules/rule-1": `{"id":"rule-1","entity_id":"entity-1"}`}

var writeToolFixtures = map[string]writeToolFixture{
	"add_drop_rule":    {args: `{"name":"drop debug","filters":[{"key":"attributes[\"level\"]","value":"debug","operator":"equals","conjunction":"and"}]}`},
	"create_dashboard": {args: `{"dashboard":` + testDashboard + `,"metadata":{"_category":"custom","_type":"metrics"}}`},
	"update_dashboard": {args: `{"id":"dash-1","dashboard":` + testDashboard + `}`},
	"patch_dashboard": {
		args:  `{"id":"dash-1","ops":[{"op":"set_title","title":"Checkout v2"}]}`,
		reads: map[string]string{"/dashboards/dash-1": `{"dashboard":` + testDashboard + `}`},
	},
	"delete_dashboard":          {args: `{"id":"dash-1"}`},
	"delete_dashboard_snapshot": {args: `{"id":"snap-1"}`},
	"create_alert": {
		args:  `{"entity_id":"entity-1","alert_rule":` + testAlertRule + `}`,
		reads: map[string]string{"/entities/entity-1/alert-rules": `{"rules":[]}`},
	},
	"update_alert":     {args: `{"entity_id":"entity-1","id":"rule-1","alert_rule":` + testAlertRule + `}`, reads: ruleOwnedByEntity},
	"patch_alert":      {args: `{"entity_id":"entity-1","id":"rule-1","is_disabled":true}`, reads: ruleOwnedByEntity},
	"delete_alert":     {args: `{"entity_id":"entity-1","id":"rule-1"}`, reads: ruleOwnedByEntity},
	"mute_alert_group": {args: `{"entity_id":"entity-1","until":1900000000}`},
	"acknowledge_alert": {
		args:  `{"alert_group_id":"group-1","rule_id":"rule-1","rule_type":"static"}`,
		reads: map[string]string{"/alerts/monitor": `{"alert_rules":[{"alert_group_id":"group-1","rule_id":"rule-1","rule_type":"static"}]}`},
	},
	"reassign_alert":              {args: `{"alert_group_id":"group-1","rule_id":"rule-1","rule_type":"static","owner_user_id":"user-1"}`},
	"create_notification_channel": {args: `{"name":"oncall","type":"slack","destination":"https://hooks.slack.com/services/x"}`},
	"attach_notification_channel": {
		args:  `{"channel_id":7,"alert_group_id":"group-1","severity":"breach"}`,
		reads: map[string]string{"/notification_settings/": `[{"id":7,"name":"oncall","service_fqid":"","property":null}]`},
	},
	"detach_notification_channel": {
		args:  `{"attachment_id":8}`,
		reads: map[string]string{"/notification_settings/": `[{"id":8,"name":"oncall","service_fqid":"group-1","severity":"breach"}]`},
	},
	"delete_notification_channel": {
		args:  `{"channel_id":7}`,
		reads: map[string]string{"/notification_settings/": `[{"id":7,"name":"oncall","type":"slack"}]`},
	},
	"create_alert_from_chart": {args: `{"surface":"discover-service","chart_key":"error_rate","service_name":"checkout","signal_key":"error_rate","name":"checkout errors"}`},
}

func writeToolTestConfig(baseURL string) models.Config {
	return models.Config{
		APIBaseURL:   baseURL,
		ActionURL:    baseURL,
		Region:       "us-east-1",
		ClusterID:    "cluster-1",
		OrgSlug:      "test-org",
		TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
	}
}

// connectTestSession serves every tool against cfg and client over in-memory transports.
func connectTestSession(t *testing.T, cfg models.Config, client *http.Client) *mcp.ClientSession {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("last9-mcp-write-test", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if _, err := registerTools(server, cfg, client); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "write-test", Version: Version}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// servedWriteTools returns every served tool that writes, by annotation or by name, and
// the names of the served read-only tools.
func servedWriteTools(t *testing.T) (writes []string, reads map[string]bool) {
	t.Helper()
	session := connectTestSession(t, writeToolTestConfig("http://unused.invalid"), http.DefaultClient)
	reads = map[string]bool{}
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		readOnly := tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		if !readOnly || hasWriteVerb(tool.Name) {
			writes = append(writes, tool.Name)
		} else {
			reads[tool.Name] = true
		}
	}
	return writes, reads
}

func hasWriteVerb(name string) bool {
	for _, verb := range []string{"add_", "create_", "update_", "delete_"} {
		if strings.HasPrefix(name, verb) {
			return true
		}
	}
	return false
}

// TestWriteToolsNeverRetryAndExplainUnknownOutcomes points every write tool at an API
// that fails after the request may have landed, and requires exactly one write attempt,
// an error result, and the tool's own verify call.
func TestWriteToolsNeverRetryAndExplainUnknownOutcomes(t *testing.T) {
	writes, reads := servedWriteTools(t)
	if len(writes) == 0 {
		t.Fatal("no write tools served")
	}
	served := map[string]bool{}
	for _, name := range writes {
		served[name] = true
	}
	for name := range writeToolFixtures {
		if !served[name] {
			t.Errorf("fixture for %s, which is not a served write tool; remove it", name)
		}
	}

	// The production client with a short timeout, so a retrying transport would show.
	client := *auth.GetHTTPClient()
	client.Timeout = 300 * time.Millisecond

	for _, name := range writes {
		fixture, ok := writeToolFixtures[name]
		if !ok {
			t.Errorf("write tool %s has no entry in writeToolFixtures; add its arguments and the GETs it makes", name)
			continue
		}
		for _, fault := range writefault.All {
			t.Run(name+"/"+string(fault), func(t *testing.T) {
				t.Parallel()
				var writesSeen atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						writesSeen.Add(1)
						writefault.Serve(w, r, fault)
						return
					}
					body, ok := fixture.reads[r.URL.Path]
					if !ok {
						t.Errorf("unexpected GET %s; add it to the %s fixture", r.URL.Path, name)
						http.NotFound(w, r)
						return
					}
					_, _ = w.Write([]byte(body))
				}))
				defer srv.Close()

				session := connectTestSession(t, writeToolTestConfig(srv.URL), &client)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(fixture.args)})
				if err != nil {
					t.Fatalf("call: %v", err)
				}
				if n := writesSeen.Load(); n != 1 {
					t.Errorf("server saw %d write requests, want exactly 1", n)
				}
				text := toolResultText(result)
				if !result.IsError {
					t.Fatalf("unknown outcome reported as success: %s", text)
				}
				wantPrefix := name + " may or may not have been applied ("
				if !strings.HasPrefix(text, wantPrefix) {
					t.Fatalf("result does not start with %q:\n%s", wantPrefix, text)
				}
				_, verify, found := strings.Cut(text, "Do not retry until you have checked: ")
				if !found {
					t.Fatalf("result names no verify call:\n%s", text)
				}
				if !reads[verifyToolName(verify)] {
					t.Fatalf("verify call %q does not start with a served read-only tool", verify)
				}
			})
		}
	}
}

// A lost chart create response leaves no rule id to disable, so a rule the check finds is live.
func TestCreateAlertFromChartLostResponseSaysFoundRuleIsLive(t *testing.T) {
	client := *auth.GetHTTPClient()
	client.Timeout = 300 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writefault.Serve(w, r, writefault.Truncated)
	}))
	defer srv.Close()

	session := connectTestSession(t, writeToolTestConfig(srv.URL), &client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_alert_from_chart",
		Arguments: json.RawMessage(writeToolFixtures["create_alert_from_chart"].args),
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	text := toolResultText(result)
	want := "if it finds the rule, it was not disabled and can fire: run patch_alert with its Entity ID, id and is_disabled=true"
	if !result.IsError || !strings.Contains(text, want) {
		t.Fatalf("result does not contain %q:\n%s", want, text)
	}
}

func toolResultText(result *mcp.CallToolResult) string {
	var parts []string
	for _, c := range result.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
