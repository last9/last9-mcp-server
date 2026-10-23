package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func streamSearchServer(t *testing.T, lines []summaryTestLine) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	body, err := json.Marshal(map[string]any{"query_result": streamsResult(lines)})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func callGetLogs(t *testing.T, srv *httptest.Server, args GetLogsArgs) map[string]any {
	t.Helper()
	args.LogjsonQuery = []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}
	args.StartTimeISO = "2023-11-14T22:13:20Z"
	args.EndTimeISO = "2023-11-14T22:23:20Z"
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("get_logs: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func timeoutLines(n int) []summaryTestLine {
	return repeatedLines(n, "ERROR", func(i int) string { return "upstream timeout after " + strings.Repeat("9", i%3+1) + " ms" })
}

func TestGetLogs_DefaultsToSummary(t *testing.T) {
	srv, _ := streamSearchServer(t, timeoutLines(30))
	out := callGetLogs(t, srv, GetLogsArgs{})
	if _, ok := out["data"]; ok {
		t.Error("default response carries raw data, want a summary")
	}
	summary, _ := out["summary"].(map[string]any)
	if summary["lines_summarized"] != float64(30) {
		t.Errorf("summary = %v, want 30 lines summarized", summary)
	}
}

func TestGetLogs_SummaryMarksLimitTruncation(t *testing.T) {
	for _, tc := range []struct {
		limit         int
		wantTruncated bool
	}{{limit: 0}, {limit: 30, wantTruncated: true}} {
		t.Run(fmt.Sprintf("limit=%d", tc.limit), func(t *testing.T) {
			srv, _ := streamSearchServer(t, timeoutLines(30))
			out := callGetLogs(t, srv, GetLogsArgs{Limit: tc.limit})
			summary, _ := out["summary"].(map[string]any)
			if got := summary["truncated"] == true; got != tc.wantTruncated || (out["l9_guidance"] != nil) != tc.wantTruncated {
				t.Errorf("summary.truncated = %v, l9_guidance = %v; want truncated %v in both", summary["truncated"], out["l9_guidance"], tc.wantTruncated)
			}
		})
	}
}

func TestGetLogs_RawModeReturnsLines(t *testing.T) {
	srv, _ := streamSearchServer(t, timeoutLines(30))
	out := callGetLogs(t, srv, GetLogsArgs{ResponseMode: "raw"})
	if _, ok := out["summary"]; ok {
		t.Error("raw mode summarized")
	}
	data, _ := out["data"].(map[string]any)
	items, _ := data["result"].([]any)
	if len(items) != 1 || countLogEntriesInResultItems(items) != 30 {
		t.Errorf("raw data = %v, want the 30 lines", data)
	}
}

func TestGetLogs_RejectsUnknownResponseModeBeforeCalling(t *testing.T) {
	srv, requests := streamSearchServer(t, timeoutLines(30))
	_, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}},
		ResponseMode: "lines",
	})
	if err == nil || !strings.Contains(err.Error(), `response_mode must be "summary" or "raw", got "lines"`) {
		t.Errorf("err = %v", err)
	}
	if *requests != 0 {
		t.Errorf("%d upstream requests, want none", *requests)
	}
}

func TestGetLogs_SummaryFitsBudgetWithCustomSeverities(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(fmt.Sprintf("wide=%v", wide), func(t *testing.T) {
			lines := timeoutLines(30)
			for i := range lines {
				lines[i].severity = fmt.Sprintf("CUSTOM_%02d", i)
				if wide {
					lines[i].severity += strings.Repeat("x", 160)
				}
			}
			srv, _ := streamSearchServer(t, lines)
			out := callGetLogs(t, srv, GetLogsArgs{})
			summary, _ := out["summary"].(map[string]any)
			if summary["lines_summarized"] != float64(len(lines)) {
				t.Fatalf("summary = %v, want %d lines summarized", summary, len(lines))
			}
			if n := utf8.RuneCountInString(formatJSON(out)); n > maxSummaryChars {
				t.Fatalf("response is %d characters, want <= %d", n, maxSummaryChars)
			}
		})
	}
}

func TestGetLogs_SummaryKeepsSeveritiesSharingAPrefix(t *testing.T) {
	var lines []summaryTestLine
	for i := range 7 {
		severity := strings.Repeat("x", 31) + fmt.Sprintf("_LEVEL_%d", i)
		lines = append(lines, repeatedLines(7-i, severity, func(int) string { return "request completed" })...)
	}
	srv, _ := streamSearchServer(t, lines)

	summary := decodeSummary(t, callGetLogs(t, srv, GetLogsArgs{}))
	labels := len(summary.SeverityCounts)
	if summary.OtherSeverities != nil {
		labels += summary.OtherSeverities.Labels
	}
	if labels != 7 {
		t.Errorf("summary represents %d severity labels, want 7: counts=%v other=%+v", labels, summary.SeverityCounts, summary.OtherSeverities)
	}
	for label, n := range summary.SeverityCounts {
		if n > 7 {
			t.Errorf("label %q has count %d; no input severity occurs more than 7 times", label, n)
		}
	}
}

func TestGetLogsInputSchema_DocumentsResponseMode(t *testing.T) {
	prop, _ := GetLogsInputSchema()["properties"].(map[string]interface{})["response_mode"].(map[string]interface{})
	if prop == nil {
		t.Fatal("response_mode missing from the get_logs schema")
	}
	if types, _ := prop["type"].([]string); strings.Join(types, ",") != "string,null" {
		t.Errorf("type = %v, want string,null", prop["type"])
	}
	desc, _ := prop["description"].(string)
	for _, want := range []string{`"summary" (default`, `"raw"`} {
		if !strings.Contains(desc, want) {
			t.Errorf("description %q does not state %s", desc, want)
		}
	}
}
