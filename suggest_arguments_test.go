package main

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSuggestionArgumentAliases(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !maps.Equal(body, map[string]string{"q": "example", "type": "service"}) {
			t.Errorf("unexpected suggestion request %v (decode error %v)", body, err)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		io.WriteString(w, `{"suggestions":[]}`)
	}))
	t.Cleanup(upstream.Close)
	cfg := testToolRegistrationConfig()
	cfg.APIBaseURL = upstream.URL
	session := connectTestSession(t, cfg, upstream.Client())
	for _, tc := range []struct {
		args, want string
		invalid    bool
	}{
		{`{"query":"example","type":"service"}`, "No suggestions found", false},
		{`{"name":"example","entity_type":"service"}`, "No suggestions found", false},
		{`{"value":"example","type":"service"}`, "No suggestions found", false},
		{`{"query":"example","kind":"service"}`, "No suggestions found", false},
		{`{"name":"example","value":"example","entity_type":"service","kind":"service"}`, "No suggestions found", false},
		{`{"name":"example","value":"different"}`, "conflict", true},
		{`{"query":"example","entity_type":"host","kind":"service"}`, "conflict", true},
		{`{"value":42}`, `argument "value" must be a string`, true},
		{`{"query":"example","kind":null}`, `argument "kind" must be a string`, true},
		{`{"query":"example","name":"example","entity_type":"service"}`, "No suggestions found", false},
		{`{"query":"example","name":"different"}`, "conflict", true},
		{`{"query":"example","type":"host","entity_type":"service"}`, "conflict", true},
		{`{"type":"service","entity_type":"service","name":"example"}`, "No suggestions found", false},
		{`{"query":"example","name":null}`, "conflict", true},
		{`{"name":null}`, `argument "name" must be a string`, true},
		{`{"entity_type":"service"}`, "", true},
		{`{"name":42}`, `argument "name" must be a string`, true},
		{`{"query":"example","entity_type":["service"]}`, `argument "entity_type" must be a string`, true},
		{`{"name":"example","unknown":true}`, "", true},
	} {
		before := requests.Load()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "did_you_mean", Arguments: json.RawMessage(tc.args)})
		wantRequests := int32(1)
		if tc.invalid {
			wantRequests = 0
		}
		if got := requests.Load() - before; got != wantRequests {
			t.Errorf("%s: got %d requests, want %d", tc.args, got, wantRequests)
		}
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
