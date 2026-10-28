package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
			handler := func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				<-ctx.Done()
				switch outcome {
				case "error":
					return nil, nil, fmt.Errorf("query failed: %w", ctx.Err())
				case "partial result":
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "partial results"}}}, nil, nil
				case "unknown write outcome":
					return nil, nil, fmt.Errorf("create failed: %w", &utils.WriteOutcomeUnknownError{Cause: "the request timed out", Err: ctx.Err()})
				default:
					return utils.ToolErrorResult("invalid response"), nil, nil
				}
			}
			fixture := registeredToolFixture{
				tool:    &mcp.Tool{Name: "get_foo", Annotations: readOnlyTool("Get Foo")},
				timeout: 20 * time.Millisecond,
			}
			want := toolErrorExpectation{
				contains: []string{"tool-call deadline", "Narrow the time range or add filters before retrying"},
				absent:   []string{"partial results", "invalid response"},
				system:   true,
			}
			if outcome == "error" {
				want.contains = append(want.contains, "query failed: context deadline exceeded")
			}
			if outcome == "unknown write outcome" {
				fixture.tool = &mcp.Tool{Name: "create_foo", Annotations: writeTool("Create Foo", false, false)}
				fixture.verify = "get_foos, by name"
				want = toolErrorExpectation{
					contains: []string{"may or may not have been applied", "the request timed out", "Do not retry until you have checked: get_foos, by name"},
					absent:   []string{"tool-call deadline", "Narrow the time range"},
				}
			}
			session := connectToolFixture(t, fixture, handler)
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: fixture.tool.Name, Arguments: struct{}{}})
			assertToolError(t, result, want)
		})
	}
}

func TestToolErrorsExportSDKErrorType(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusServiceUnavailable, "system_error"},
		{http.StatusBadRequest, "user_error"},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			cfg := writeToolTestConfig("http://upstream.invalid")
			cfg.UseLogSearchAPI = true
			client := &http.Client{Transport: errorTypeStatusTransport(tc.status)}
			result := callToolForErrorType(t, cfg, client, "get_logs", getLogsTestArgs)
			if !result.IsError {
				t.Fatalf("want tool error, got %+v", result)
			}
			toolSpans := 0
			for _, span := range exporter.GetSpans() {
				attrs := make(map[string]string)
				for _, attr := range span.Attributes {
					attrs[string(attr.Key)] = attr.Value.AsString()
				}
				if attrs["mcp.tool.name"] != "get_logs" {
					continue
				}
				toolSpans++
				if got := attrs["mcp.error.type"]; got != tc.want {
					t.Errorf("exported mcp.error.type = %q, want %q", got, tc.want)
				}
			}
			if toolSpans != 1 {
				t.Fatalf("exported %d get_logs spans, want 1", toolSpans)
			}
		})
	}
}

type errorTypeStatusTransport int

func (status errorTypeStatusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: int(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"error":"invalid pipeline"}`)),
		Request:    req,
	}, nil
}
