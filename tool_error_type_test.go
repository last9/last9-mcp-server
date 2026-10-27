package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const getLogsTestArgs = `{"logjson_query":[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","api"]}]}}],"lookback_minutes":5}`

// Upstream failures carry the system_error tag the SDK reads for mcp.error.type;
// caller mistakes stay untagged and so keep the default user_error.
func TestToolErrorsTagUpstreamFailuresAsSystemErrors(t *testing.T) {
	statusServer := func(status int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"unknown stage \"filtr\""}`))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	release := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(hanging.Close)
	stalledBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(stalledBody.Close)
	t.Cleanup(func() { close(release) })
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()

	cases := []struct {
		name       string
		baseURL    string
		searchAPI  bool
		client     *http.Client
		tool, args string
		wantSystem bool
	}{
		{"log search HTTP 503", statusServer(http.StatusServiceUnavailable).URL, true, http.DefaultClient, "get_logs", getLogsTestArgs, true},
		{"log search timeout", hanging.URL, true, &http.Client{Timeout: 50 * time.Millisecond}, "get_logs", getLogsTestArgs, true},
		{"log search body timeout", stalledBody.URL, true, &http.Client{Timeout: 50 * time.Millisecond}, "get_logs", getLogsTestArgs, true},
		{"chunked logs timeout", hanging.URL, false, &http.Client{Timeout: 50 * time.Millisecond}, "get_logs", getLogsTestArgs, true},
		{"chunked logs body timeout", stalledBody.URL, false, &http.Client{Timeout: 50 * time.Millisecond}, "get_logs", getLogsTestArgs, true},
		{"trace service unreachable", unreachable.URL, false, http.DefaultClient, "get_exceptions", `{}`, true},
		{"log search HTTP 400 bad pipeline", statusServer(http.StatusBadRequest).URL, true, http.DefaultClient, "get_logs", getLogsTestArgs, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := writeToolTestConfig(tc.baseURL)
			cfg.PrometheusReadURL = tc.baseURL
			cfg.UseLogSearchAPI = tc.searchAPI
			result := callToolForErrorType(t, cfg, tc.client, tc.tool, tc.args)
			if !result.IsError {
				t.Fatalf("%s: want isError result, got %+v", tc.tool, result)
			}
			gotSystem := result.Meta[utils.ErrorTypeMetaKey] == utils.SystemErrorType
			if gotSystem != tc.wantSystem {
				t.Fatalf("%s: system tag = %v, want %v (meta %v, text %q)", tc.tool, gotSystem, tc.wantSystem, result.Meta, result.Content[0].(*mcp.TextContent).Text)
			}
		})
	}
}

func callToolForErrorType(t *testing.T, cfg models.Config, client *http.Client, tool, args string) *mcp.CallToolResult {
	t.Helper()
	session := connectTestSession(t, cfg, client)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	return result
}

func TestRegisteredDeadlineErrorType(t *testing.T) {
	for _, outcome := range []string{"error", "partial result", "tool error", "unknown write outcome"} {
		t.Run(outcome, func(t *testing.T) {
			server := newRegisterTestServer(t)
			gate := newToolGate(toolsets.Filter{})
			gate.callTimeout = -time.Second
			handler := func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				switch outcome {
				case "error":
					return nil, nil, ctx.Err()
				case "partial result":
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "partial results"}}}, nil, nil
				case "unknown write outcome":
					return nil, nil, &utils.WriteOutcomeUnknownError{Err: ctx.Err()}
				default:
					return utils.ToolErrorResult("invalid response"), nil, nil
				}
			}
			tool := &mcp.Tool{Name: "get_foo", Annotations: readOnlyTool("Get Foo")}
			var err error
			if outcome == "unknown write outcome" {
				tool = &mcp.Tool{Name: "create_foo", Annotations: writeTool("Create Foo", false, false)}
				err = registerWriteTool(server, gate, tool, "get_foos, by name", handler)
			} else {
				err = registerIfAllowed(server, gate, tool, handler)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := gate.registerAdmitted(); err != nil {
				t.Fatal(err)
			}
			result, err := connectRegisteredServer(t, server).CallTool(context.Background(), &mcp.CallToolParams{Name: tool.Name, Arguments: struct{}{}})
			if err != nil {
				t.Fatal(err)
			}
			wantSystem := outcome != "unknown write outcome"
			if !result.IsError || (result.Meta[utils.ErrorTypeMetaKey] == utils.SystemErrorType) != wantSystem {
				t.Fatalf("want isError and system_error=%v, got %+v", wantSystem, result)
			}
			wantText := "tool-call deadline"
			if !wantSystem {
				wantText = "get_foos, by name"
			}
			if !strings.Contains(toolResultText(result), wantText) {
				t.Fatalf("missing %q: %s", wantText, toolResultText(result))
			}
		})
	}
}
