package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/suggest"
)

func TestSuggestionArgumentAliases(t *testing.T) {
	session := connectToolFixture(t, registeredToolFixture{tool: &mcp.Tool{Name: "did_you_mean"}}, func(_ context.Context, _ *mcp.CallToolRequest, in suggest.DidYouMeanArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Query + "/" + in.Type}}}, nil, nil
	})
	for _, tc := range []struct {
		args, want string
		invalid    bool
	}{
		{`{"query":"example","type":"service"}`, "example/service", false},
		{`{"name":"example","entity_type":"service"}`, "example/service", false},
		{`{"query":"example","name":"example","entity_type":"service"}`, "example/service", false},
		{`{"query":"example","name":"different"}`, "conflict", true},
		{`{"query":"example","type":"host","entity_type":"service"}`, "conflict", true},
		{`{"type":"service","entity_type":"service","name":"example"}`, "example/service", false},
		{`{"query":"example","name":null}`, "conflict", true},
		{`{"name":null}`, `argument "name" must be a string`, true},
		{`{"entity_type":"service"}`, "", true},
		{`{"name":42}`, `argument "name" must be a string`, true},
		{`{"query":"example","entity_type":["service"]}`, `argument "entity_type" must be a string`, true},
		{`{"name":"example","unknown":true}`, "", true},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "did_you_mean", Arguments: json.RawMessage(tc.args)})
		if err != nil {
			if tc.invalid && strings.Contains(err.Error(), tc.want) {
				continue
			}
			t.Fatalf("%s: %v", tc.args, err)
		}
		if result.IsError != tc.invalid || !strings.Contains(toolResultText(result), tc.want) {
			t.Errorf("%s: got %s", tc.args, toolResultText(result))
		}
	}
}
