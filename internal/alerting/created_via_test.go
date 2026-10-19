package alerting

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bodyRecorder answers every GET with getReply (or, when unset, the rule-ownership lookup)
// and records every write body; each write gets writeReply.
type bodyRecorder struct {
	*httptest.Server
	mu       sync.Mutex
	bodies   []map[string]any
	getReply string
}

func newBodyRecorder(t *testing.T, writeReply string) *bodyRecorder {
	t.Helper()
	rec := &bodyRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodGet {
			if rec.getReply != "" {
				_, _ = w.Write([]byte(rec.getReply))
				return
			}
			if serveRuleOwner(w, r) {
				return
			}
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("%s %s body is not a JSON object: %s", r.Method, r.URL.Path, raw)
		}
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, body)
		rec.mu.Unlock()
		_, _ = w.Write([]byte(writeReply))
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *bodyRecorder) only(t *testing.T) map[string]any {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.bodies) != 1 {
		t.Fatalf("writes = %d, want 1: %v", len(rec.bodies), rec.bodies)
	}
	return rec.bodies[0]
}

func ruleAnnotations(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	props, _ := body["properties"].(map[string]any)
	annotations, _ := props["annotations"].(map[string]any)
	return annotations
}

func clientRequest(name, version string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{
		mcp.MetaKeyClientInfo: &mcp.Implementation{Name: name, Version: version},
	}}}
}

func TestCreatedViaClient(t *testing.T) {
	long := strings.Repeat("a", 200)
	for _, tt := range []struct {
		name string
		req  *mcp.CallToolRequest
		want string
	}{
		{"nil request", nil, "unknown/unknown"},
		{"no client info", &mcp.CallToolRequest{}, "unknown/unknown"},
		{"name and version", clientRequest("claude-code", "2.1.0"), "claude-code/2.1.0"},
		{"missing version", clientRequest("cursor", ""), "cursor/unknown"},
		{"missing name", clientRequest("", "1.0"), "unknown/1.0"},
		{"blank name", clientRequest(" \t", "1.0"), "unknown/1.0"},
		{"template delimiters", clientRequest("ev{{ .Labels }}il", "1}}{{0"), "ev .Labels il/10"},
		{"delimiter joined by removal", clientRequest("a{}}{b", "1"), "ab/1"},
		{"control chars", clientRequest("bad\nname\x00", "1\r\t0"), "badname/10"},
		{"only delimiters", clientRequest("{{}}", "{{"), "unknown/unknown"},
		{"invalid utf-8", clientRequest("ok\xffname", "1"), "okname/1"},
		{"truncated", clientRequest(long, "9"), long[:128]},
		{"truncated by rune", clientRequest(strings.Repeat("é", 127), "12"), strings.Repeat("é", 127) + "/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := createdViaClient(tt.req); got != tt.want {
				t.Fatalf("createdViaClient = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateAlertStampsCreatedViaMarker(t *testing.T) {
	for _, tt := range []struct {
		name  string
		props *AlertProperties
		want  map[string]any
	}{
		{"no properties", nil, map[string]any{}},
		{"caller annotations kept", &AlertProperties{Description: "d", Annotations: map[string]string{"team": "core"}}, map[string]any{"team": "core"}},
		{"caller marker overridden", &AlertProperties{Annotations: map[string]string{
			annotationCreatedVia: "dashboard", annotationCreatedViaClient: "forged/1",
		}}, map[string]any{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := newBodyRecorder(t, `{"id":"rule-1"}`)
			payload := validAlertRulePayload(nil)
			payload.Properties = tt.props
			var before map[string]string
			if tt.props != nil {
				before = maps.Clone(tt.props.Annotations)
			}
			result, _, err := NewCreateAlertHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(),
				clientRequest("claude-code", "2.1.0"), CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: payload}})
			if err != nil || result.IsError {
				t.Fatalf("result=%s err=%v", resultText(result), err)
			}
			got := ruleAnnotations(t, rec.only(t))
			want := map[string]any{annotationCreatedVia: "last9-mcp", annotationCreatedViaClient: "claude-code/2.1.0"}
			for k, v := range tt.want {
				want[k] = v
			}
			if !mapsEqual(got, want) {
				t.Fatalf("annotations = %v, want %v", got, want)
			}
			if tt.props != nil && !maps.Equal(tt.props.Annotations, before) {
				t.Fatalf("caller's annotations were mutated: %v", tt.props.Annotations)
			}
		})
	}
}

// The client string must come from the MCP session's initialize handshake.
func TestCreateAlertReadsClientFromSession(t *testing.T) {
	rec := newBodyRecorder(t, `{"id":"rule-1"}`)
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "create_alert"}, NewCreateAlertHandler(rec.Client(), testAlertMutationConfig(rec.URL)))
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "codex", Version: "0.9"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_alert", Arguments: map[string]any{
		"entity_id":  "entity-1",
		"alert_rule": map[string]any{"primary_indicator": "latency", "expression_args": map[string]any{"latency": map[string]any{"id": "kpi-1"}}},
	}})
	if err != nil || result.IsError {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
	if got := ruleAnnotations(t, rec.only(t))[annotationCreatedViaClient]; got != "codex/0.9" {
		t.Fatalf("%s = %v, want codex/0.9", annotationCreatedViaClient, got)
	}
}

func TestRuleEditsKeepStoredMarkerAndDropForgedOnes(t *testing.T) {
	const marked = `{"id":"rule-1","entity_id":"entity-1","properties":{"annotations":{"_l9_created_via_":"last9-mcp","_l9_created_via_client_":"claude-code/2.1.0","team":"old"}}}`
	const unmarked = `{"id":"rule-1","entity_id":"entity-1","properties":{"annotations":{"team":"old"}}}`
	storedMarker := map[string]any{annotationCreatedVia: "last9-mcp", annotationCreatedViaClient: "claude-code/2.1.0"}
	forged := func() *AlertProperties {
		return &AlertProperties{Annotations: map[string]string{
			"team": "new", annotationCreatedVia: "last9-mcp", annotationCreatedViaClient: "forged/1",
		}}
	}
	for _, tt := range []struct {
		name   string
		stored string
		props  *AlertProperties
		want   map[string]any
	}{
		{"marked, caller forges", marked, forged(), map[string]any{"team": "new", annotationCreatedVia: "last9-mcp", annotationCreatedViaClient: "claude-code/2.1.0"}},
		{"marked, caller sends no annotations", marked, &AlertProperties{Description: "d"}, storedMarker},
		{"unmarked, caller forges", unmarked, forged(), map[string]any{"team": "new"}},
		{"stored annotation values not strings", `{"id":"rule-1","entity_id":"entity-1","properties":{"annotations":{"_l9_created_via_":"last9-mcp","n":3}}}`, &AlertProperties{}, map[string]any{annotationCreatedVia: "last9-mcp"}},
	} {
		for tool, call := range map[string]func(*bodyRecorder) (*mcp.CallToolResult, error){
			"update_alert": func(rec *bodyRecorder) (*mcp.CallToolResult, error) {
				payload := validAlertRulePayload(nil)
				payload.Properties = tt.props
				r, _, err := NewUpdateAlertHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), clientRequest("other", "9"),
					UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: payload}})
				return r, err
			},
			"patch_alert": func(rec *bodyRecorder) (*mcp.CallToolResult, error) {
				r, _, err := NewPatchAlertHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), clientRequest("other", "9"),
					PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", Properties: tt.props})
				return r, err
			},
		} {
			t.Run(tt.name+"/"+tool, func(t *testing.T) {
				rec := newBodyRecorder(t, `{"id":"rule-2"}`)
				rec.getReply = tt.stored
				result, err := call(rec)
				if err != nil || result.IsError {
					t.Fatalf("result=%s err=%v", resultText(result), err)
				}
				if got := ruleAnnotations(t, rec.only(t)); !mapsEqual(got, tt.want) {
					t.Fatalf("annotations = %v, want %v", got, tt.want)
				}
			})
		}
	}
}

func TestPatchWithoutPropertiesLeavesAnnotationsAlone(t *testing.T) {
	rec := newBodyRecorder(t, `{}`)
	rec.getReply = `{"id":"rule-1","entity_id":"entity-1","properties":{"annotations":{"_l9_created_via_":"last9-mcp"}}}`
	enabled := false
	result, _, err := NewPatchAlertHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), nil,
		PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &enabled})
	if err != nil || result.IsError {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
	if body := rec.only(t); body["properties"] != nil {
		t.Fatalf("patch sent properties it was not asked to change: %v", body)
	}
}

func TestUpdateWithoutPropertiesKeepsStoredMarker(t *testing.T) {
	rec := newBodyRecorder(t, `{"id":"rule-2"}`)
	rec.getReply = `{"id":"rule-1","entity_id":"entity-1","properties":{"annotations":{"_l9_created_via_":"last9-mcp","_l9_created_via_client_":"c/1"}}}`
	result, _, err := NewUpdateAlertHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), nil,
		UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	if err != nil || result.IsError {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
	want := map[string]any{annotationCreatedVia: "last9-mcp", annotationCreatedViaClient: "c/1"}
	if got := ruleAnnotations(t, rec.only(t)); !mapsEqual(got, want) {
		t.Fatalf("annotations = %v, want %v", got, want)
	}
}

func TestChartAndChannelCreatesSendCreatedVia(t *testing.T) {
	req := clientRequest("claude-code{{x}}", "2.1.0\n")
	for _, tt := range []struct {
		name, getReply, writeReply string
		writes                     int
		call                       func(*bodyRecorder) (*mcp.CallToolResult, error)
	}{
		{"create_alert_from_chart", "", `{"id":"rule-9"}`, 1, func(rec *bodyRecorder) (*mcp.CallToolResult, error) {
			r, _, err := NewCreateAlertFromChartHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), req, CreateAlertFromChartArgs{
				Surface: "discover-service", ChartKey: "error_rate", ServiceName: "checkout", SignalKey: "error_ratio", Name: "n",
			})
			return r, err
		}},
		{"create_notification_channel", "", `{"id":7,"name":"n","type":"email"}`, 1, func(rec *bodyRecorder) (*mcp.CallToolResult, error) {
			r, _, err := NewCreateNotificationChannelHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), req, CreateNotificationChannelArgs{
				Name: "n", Type: "email", Destination: "a@b.co",
			})
			return r, err
		}},
		{"attach_notification_channel", `[{"id":7,"name":"n","type":"email"}]`, `{"id":11}`, 2, func(rec *bodyRecorder) (*mcp.CallToolResult, error) {
			r, _, err := NewAttachNotificationChannelHandler(rec.Client(), testAlertMutationConfig(rec.URL))(context.Background(), req, AttachNotificationChannelArgs{
				ChannelID: 7, AlertGroupID: "g1", Severity: "both",
			})
			return r, err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := newBodyRecorder(t, tt.writeReply)
			rec.getReply = tt.getReply
			result, err := tt.call(rec)
			if err != nil || result.IsError {
				t.Fatalf("result=%s err=%v", resultText(result), err)
			}
			if len(rec.bodies) != tt.writes {
				t.Fatalf("writes = %d, want %d", len(rec.bodies), tt.writes)
			}
			for _, body := range rec.bodies {
				if body["created_via"] != "last9-mcp" || body["created_via_client"] != "claude-codex/2.1.0" {
					t.Fatalf("body = %v", body)
				}
			}
		})
	}
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
