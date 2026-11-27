package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetExceptionsOptionalNulls(t *testing.T) {
	type request struct {
		Query     string
		Timestamp int64
	}
	requests := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got request
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if r.URL.Path == constants.EndpointPromQueryInstant {
			requests <- got
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()
	session := connectTestSession(t, writeToolTestConfig(server.URL), server.Client())
	for _, tc := range []struct {
		name           string
		extra          map[string]any
		window, filter string
	}{
		{"omitted", nil, "[900s]", ""},
		{"empty strings", map[string]any{"span_name": "", "start_time_iso": ""}, "[900s]", ""},
		{"null span", map[string]any{"span_name": nil}, "[900s]", ""},
		{"null start", map[string]any{"start_time_iso": nil}, "[900s]", ""},
		{"both null", map[string]any{"span_name": nil, "start_time_iso": nil}, "[900s]", ""},
		{"concrete values", map[string]any{"span_name": "example", "start_time_iso": "2026-01-01T00:05:00Z"}, "[1500s]", "span_name=~'example'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"lookback_minutes": 15, "end_time_iso": "2026-01-01T00:30:00Z"}
			for k, v := range tc.extra {
				args[k] = v
			}
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_exceptions", Arguments: args})
			if result.IsError {
				t.Fatal(toolResultText(result))
			}
			select {
			case got := <-requests:
				if got.Timestamp != 1767227400 || !strings.Contains(got.Query, tc.window) {
					t.Errorf("wrong query window: %+v", got)
				}
				if tc.filter == "" && strings.Contains(got.Query, "span_name=~") || tc.filter != "" && !strings.Contains(got.Query, tc.filter) {
					t.Errorf("unexpected span filter: %s", got.Query)
				}
			default:
				t.Fatal("expected an upstream query")
			}
		})
	}
}
