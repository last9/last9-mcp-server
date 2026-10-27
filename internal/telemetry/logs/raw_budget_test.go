package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRawLogResponseByteBudget(t *testing.T) {
	for _, service := range []bool{false, true} {
		for _, oversized := range []bool{false, true} {
			name := "get_logs"
			if service {
				name = "get_service_logs"
			}
			if oversized {
				name += "/oversized-entry"
			}
			t.Run(name, func(t *testing.T) {
				count, width := 30, 2000
				if oversized {
					count, width = 1, 50000
				}
				lines := repeatedLines(count, "ERROR", func(int) string { return strings.Repeat("é\"\\\n", width) })
				srv, _ := streamSearchServer(t, lines)
				var result *mcp.CallToolResult
				var err error
				if service {
					result, _, err = NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{ServiceName: "api", Limit: 50, ResponseMode: "raw", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
				} else {
					result, _, err = NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}, Limit: 50, ResponseMode: "raw", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
				}
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if len(encoded) > 32000 {
					t.Fatalf("serialized response %d bytes exceeds 32000", len(encoded))
				}
				if oversized {
					if !result.IsError {
						t.Fatal("oversized first entry must return an explicit error")
					}
					return
				}
				if result.IsError {
					t.Fatal("fitting prefix should return entries")
				}
				var out map[string]any
				if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
					t.Fatal(err)
				}
				if out["logs_truncated"] != true {
					t.Fatal("missing truncation marker")
				}
				if service {
					entries := out["logs"].([]any)
					kept := len(entries)
					if kept == 0 || kept >= count || out["count"] != float64(kept) {
						t.Fatalf("incorrect count: %v", out["count"])
					}
					if entries[0].(map[string]any)["message"] != lines[0].message {
						t.Fatal("message clipped")
					}
					coverage := result.Meta["last9/coverage"].(map[string]any)
					if coverage["status"] != "partial" || len(coverage["covered_scope"].(map[string]any)) != 0 {
						t.Fatalf("truncated coverage must be partial: %v", coverage)
					}
				} else {
					_, items, err := extractResultItems(out)
					if err != nil {
						t.Fatal(err)
					}
					kept := countLogEntriesInResultItems(items)
					if kept == 0 || kept >= count {
						t.Fatalf("invalid count %d", kept)
					}
					values := items[0].(map[string]any)["values"].([]any)
					if values[0].([]any)[1] != lines[0].message {
						t.Fatal("message clipped")
					}
				}
			})
		}
	}
}

func TestBoundRawResultPreservesSmallAndRejectsOversizedMetadata(t *testing.T) {
	small := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "small"}}}
	got, err := boundRawResult(small, 1, nil, func(int) (*mcp.CallToolResult, error) { t.Fatal("small result changed"); return nil, nil })
	if err != nil || got != small {
		t.Fatalf("small result changed: %v", err)
	}
	huge := &mcp.CallToolResult{Meta: mcp.Meta{"key": strings.Repeat("x", 40000)}, Content: small.Content}
	got, err = boundRawResult(huge, 1, nil, func(int) (*mcp.CallToolResult, error) { return huge, nil })
	if err != nil || !got.IsError || got.Meta != nil {
		t.Fatal("oversized metadata must produce compact error")
	}
}

func TestDefaultSmallRawResponsesAreBounded(t *testing.T) {
	lines := repeatedLines(2, "ERROR", func(int) string { return strings.Repeat("x", 20000) })
	srv, _ := streamSearchServer(t, lines)
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > 32000 {
		t.Fatalf("default raw result exceeds budget: %d: %v", len(data), err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	if out["logs_truncated"] != true {
		t.Fatal("missing truncation marker")
	}
}

func TestRawDefaultFiveThousandLines(t *testing.T) {
	lines := repeatedLines(5000, "ERROR", func(int) string { return strings.Repeat("x", 100) })
	srv, _ := streamSearchServer(t, lines)
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}, ResponseMode: "raw", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxRawResultBytes || result.IsError {
		t.Fatalf("invalid bounded result: %d bytes, %v", len(encoded), err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	limit := out["l9_raw_limit"].(map[string]any)
	if limit["fetched_entries"] != float64(5000) || limit["returned_entries"].(float64) >= 5000 {
		t.Fatalf("incorrect fetched/returned counts: %v", limit)
	}
}

func TestRawStreamPrefixPreservesTotals(t *testing.T) {
	lines := repeatedLines(30, "ERROR", func(i int) string { return fmt.Sprintf("%d-%s", i, strings.Repeat("x", 4000)) })
	for i := 3; i < len(lines); i++ {
		lines[i].severity = "INFO"
	}
	original := streamsResult(lines)
	body, err := json.Marshal(map[string]any{"query_result": original, "total_matching_lines": 1000})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}, Limit: 50, ResponseMode: "raw", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	_, items, err := extractResultItems(out)
	if err != nil {
		t.Fatal(err)
	}
	_, source, err := extractResultItems(original)
	if err != nil {
		t.Fatal(err)
	}
	kept := countLogEntriesInResultItems(items)
	if len(items) != 2 || !reflect.DeepEqual(items, truncateResultItemsByEntryLimit(source, kept)) {
		t.Fatal("returned entries differ from the whole-entry stream prefix")
	}
	if out["total_matching_lines"] != float64(1000) {
		t.Fatalf("total matching lines changed: %v", out["total_matching_lines"])
	}
	limit := out["l9_raw_limit"].(map[string]any)
	if limit["returned_entries"] != float64(kept) || limit["fetched_entries"] != float64(30) {
		t.Fatalf("wrong byte-limit counts: %v", limit)
	}
}

func TestServiceLogsSmallSummaryModeBounded(t *testing.T) {
	for _, mode := range []string{"", "summary"} {
		t.Run(mode, func(t *testing.T) {
			lines := repeatedLines(2, "ERROR", func(int) string { return strings.Repeat("x", 20000) })
			srv, _ := streamSearchServer(t, lines)
			result, _, err := NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{ServiceName: "api", ResponseMode: mode, StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > maxRawResultBytes || result.IsError {
				t.Fatalf("invalid result: %d bytes, %v", len(encoded), err)
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
				t.Fatal(err)
			}
			if out["logs_truncated"] != true || out["count"] != float64(1) {
				t.Fatalf("incorrect truncation/count: %v", out["count"])
			}
		})
	}
}

func TestRawHandlerOversizedMetadataReturnsError(t *testing.T) {
	srv, _ := streamSearchServer(t, timeoutLines(1))
	cfg := apiModeCfg(srv.URL)
	cfg.OrgSlug = strings.Repeat("x", 40000)
	result, _, err := NewGetLogsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}, ResponseMode: "raw", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !result.IsError || result.Meta != nil || len(encoded) > maxRawResultBytes {
		t.Fatal("oversized metadata must return a compact error")
	}
}

func TestAggregateResultIsNotRawBudgeted(t *testing.T) {
	body, err := json.Marshal(map[string]any{"query_result": map[string]any{"data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": map[string]any{"label": strings.Repeat("x", 40000)}, "values": []any{}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: []map[string]interface{}{{"type": "aggregate", "aggregates": []any{map[string]any{"function": map[string]any{"$count": []any{}}, "as": "count"}}}}, ResponseMode: "raw", StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || result.IsError || len(encoded) <= maxRawResultBytes {
		t.Fatalf("aggregate was capped: %d bytes, %v", len(encoded), err)
	}
}

func TestRawBudgetIncludesSDKWireResultType(t *testing.T) {
	full := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", 31900)}}}
	encoded, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	full.Content[0].(*mcp.TextContent).Text += strings.Repeat("x", maxRawResultBytes-len(encoded)-sdkResultTypeBytes-1)
	encoded, err = json.Marshal(full)
	if err != nil || len(encoded) != maxRawResultBytes-sdkResultTypeBytes-1 {
		t.Fatalf("bad near-boundary fixture: %d %v", len(encoded), err)
	}
	info := &mcp.Implementation{Name: "last9-mcp", Version: "v1.123.456-long-\"build\\version"}
	server := mcp.NewServer(info, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "bounded"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		bounded, err := boundRawResult(full, 1, info, func(int) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "truncated"}}}, nil
		})
		return bounded, nil, err
	})
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "budget-client", Version: "1"}, nil).Connect(ctx, clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "bounded", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(result)
	if err != nil || len(encoded) > maxRawResultBytes {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(encoded, &fields)
		for k, v := range fields {
			t.Logf("field %s: %d bytes", k, len(v))
			if k == "_meta" {
				t.Logf("meta: %s", v)
			}
		}
		t.Fatalf("wire result exceeds cap: %d %v", len(encoded), err)
	}
	if result.Content[0].(*mcp.TextContent).Text != "truncated" {
		t.Fatal("near-boundary result was not bounded before SDK added metadata")
	}
	if !strings.Contains(string(encoded), `"resultType":"complete"`) {
		t.Fatal("test did not exercise the latest protocol resultType")
	}
}
