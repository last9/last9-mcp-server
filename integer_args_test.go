package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type integerArgsTestInput struct {
	ChannelID int    `json:"channel_id"`
	Limit     *int   `json:"limit,omitempty"`
	Name      string `json:"name,omitempty"`
	Rule      *struct {
		EvalWindow *int `json:"eval_window,omitempty"`
	} `json:"rule,omitempty"`
	Versions []int `json:"versions,omitempty"`
}

// callIntegerArgsTool serves one tool taking integerArgsTestInput and calls it with args,
// returning the input its handler saw (nil when the call never reached it) and the result.
func callIntegerArgsTool(t *testing.T, args string) (*integerArgsTestInput, *mcp.CallToolResult) {
	t.Helper()
	server := newRegisterTestServer(t)
	var seen *integerArgsTestInput
	handler := func(_ context.Context, _ *mcp.CallToolRequest, in integerArgsTestInput) (*mcp.CallToolResult, any, error) {
		seen = &in
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
	}
	if err := registerIfAllowed(server, nil, &mcp.Tool{Name: "int_tool"}, handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "int-test", Version: Version}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "int_tool", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	return seen, res
}

func intPtr(n int) *int { return &n }

func TestIntegerArgsAcceptNumberLikeStrings(t *testing.T) {
	cases := []struct {
		name string
		args string
		want integerArgsTestInput
	}{
		{"quoted id", `{"channel_id":"42"}`, integerArgsTestInput{ChannelID: 42}},
		{"negative", `{"channel_id":"-7"}`, integerArgsTestInput{ChannelID: -7}},
		{"zero", `{"channel_id":"0"}`, integerArgsTestInput{ChannelID: 0}},
		{"leading zero", `{"channel_id":"042"}`, integerArgsTestInput{ChannelID: 42}},
		{"number untouched", `{"channel_id":42}`, integerArgsTestInput{ChannelID: 42}},
		{"nullable integer", `{"channel_id":1,"limit":"5"}`, integerArgsTestInput{ChannelID: 1, Limit: intPtr(5)}},
		{"string param keeps digits", `{"channel_id":1,"name":"42"}`, integerArgsTestInput{ChannelID: 1, Name: "42"}},
		{"array items", `{"channel_id":1,"versions":["1",2]}`, integerArgsTestInput{ChannelID: 1, Versions: []int{1, 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, res := callIntegerArgsTool(t, tc.args)
			if res.IsError || seen == nil {
				t.Fatalf("call rejected: %s", toolResultText(res))
			}
			got, _ := json.Marshal(seen)
			want, _ := json.Marshal(tc.want)
			if string(got) != string(want) {
				t.Fatalf("handler saw %s, want %s", got, want)
			}
		})
	}
}

func TestIntegerArgsCoerceNestedObjectProperties(t *testing.T) {
	seen, res := callIntegerArgsTool(t, `{"channel_id":1,"rule":{"eval_window":"5"}}`)
	if res.IsError || seen == nil {
		t.Fatalf("call rejected: %s", toolResultText(res))
	}
	if seen.Rule == nil || seen.Rule.EvalWindow == nil || *seen.Rule.EvalWindow != 5 {
		t.Fatalf("rule.eval_window = %+v, want 5", seen.Rule)
	}
}

func TestIntegerArgsRejectNonIntegerStrings(t *testing.T) {
	for _, value := range []string{"4x2", "4.2", "", " 42", "+42", "1e3", "99999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			arg, _ := json.Marshal(value)
			seen, res := callIntegerArgsTool(t, `{"channel_id":`+string(arg)+`}`)
			if seen != nil || !res.IsError {
				t.Fatalf("want a validation error, handler saw %+v", seen)
			}
			text := toolResultText(res)
			if !strings.Contains(text, "channel_id") || !strings.Contains(text, "integer") {
				t.Fatalf("error should name channel_id and its integer type: %s", text)
			}
		})
	}
}

func TestIntegerArgsKeepRejectingUnknownProperties(t *testing.T) {
	seen, res := callIntegerArgsTool(t, `{"channel_id":"42","channel":"42"}`)
	if seen != nil || !res.IsError {
		t.Fatalf("want unknown property rejected, handler saw %+v", seen)
	}
}

// TestServedToolsAcceptQuotedIntegerIDs covers the served tool from the eval that
// regressed: attach_notification_channel with channel_id "7" reaches the API as 7.
func TestServedToolsAcceptQuotedIntegerIDs(t *testing.T) {
	var mu sync.Mutex
	var writes []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(writeToolFixtures["attach_notification_channel"].reads[r.URL.Path]))
			return
		}
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":9,"name":"oncall","service_fqid":"group-1","severity":"breach"}`))
	}))
	defer api.Close()

	session := connectTestSession(t, writeToolTestConfig(api.URL), api.Client())
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "attach_notification_channel",
		Arguments: json.RawMessage(`{"channel_id":"7","alert_group_id":"group-1","severity":"breach"}`),
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("quoted channel_id rejected: %s", toolResultText(res))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 1 || writes[0] != "POST /notification_settings/7/attach" {
		t.Fatalf("writes = %v, want one POST /notification_settings/7/attach", writes)
	}
}
