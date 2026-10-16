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

// bodyRecorder answers the rule-ownership GET and records every write body; each write
// gets writeReply.
type bodyRecorder struct {
	*httptest.Server
	mu         sync.Mutex
	bodies     []map[string]any
	ruleLookup string
}

func newBodyRecorder(t *testing.T, writeReply string) *bodyRecorder {
	t.Helper()
	rec := &bodyRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodGet {
			if rec.ruleLookup != "" && strings.Contains(r.URL.Path, "/alert-rules/") {
				_, _ = w.Write([]byte(rec.ruleLookup))
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
