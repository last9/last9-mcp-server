package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callGetServiceLogs(t *testing.T, lines []summaryTestLine, args GetServiceLogsArgs) map[string]any {
	t.Helper()
	srv, _ := streamSearchServer(t, lines)
	args.ServiceName = "api"
	args.StartTimeISO = "2023-11-14T22:13:20Z"
	args.EndTimeISO = "2023-11-14T22:23:20Z"
	result, _, err := NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("get_service_logs: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if n := utf8.RuneCountInString(text); args.ResponseMode == "" && len(lines) > maxRawLinesInSummaryMode && n > maxSummaryChars {
		t.Errorf("summary response is %d characters, want <= %d", n, maxSummaryChars)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestGetServiceLogs_SummarizesOverThreshold(t *testing.T) {
	for _, tc := range []struct {
		limit         int
		wantTruncated bool
	}{{limit: 50}, {limit: 30, wantTruncated: true}} {
		t.Run(fmt.Sprintf("limit=%d", tc.limit), func(t *testing.T) {
			out := callGetServiceLogs(t, timeoutLines(30), GetServiceLogsArgs{Limit: tc.limit})
			if _, ok := out["logs"]; ok {
				t.Error("default response carries raw logs, want a summary")
			}
			if out["service"] != "api" || out["count"] != float64(30) {
				t.Errorf("service = %v, count = %v; want api and 30", out["service"], out["count"])
			}
			summary := decodeSummary(t, out)
			if summary.LinesSummarized != 30 || len(summary.Patterns) == 0 {
				t.Errorf("summary = %+v, want 30 lines in patterns", summary)
			}
			if summary.Truncated != tc.wantTruncated {
				t.Errorf("summary.truncated = %v, want %v", summary.Truncated, tc.wantTruncated)
			}
		})
	}
}

func TestGetServiceLogs_ReturnsRawLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines int
		mode  string
	}{
		{name: "raw mode over threshold", lines: 30, mode: "raw"},
		{name: "summary at threshold", lines: maxRawLinesInSummaryMode},
		{name: "explicit summary at threshold", lines: maxRawLinesInSummaryMode, mode: "summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := callGetServiceLogs(t, timeoutLines(tc.lines), GetServiceLogsArgs{Limit: 50, ResponseMode: tc.mode})
			if _, ok := out["summary"]; ok {
				t.Error("response summarized, want raw lines")
			}
			if logs, _ := out["logs"].([]any); len(logs) != tc.lines {
				t.Errorf("got %d log entries, want %d", len(logs), tc.lines)
			}
		})
	}
}

func TestGetServiceLogs_SummarizesOneOverThreshold(t *testing.T) {
	out := callGetServiceLogs(t, timeoutLines(maxRawLinesInSummaryMode+1), GetServiceLogsArgs{Limit: 50})
	if summary := decodeSummary(t, out); summary.LinesSummarized != maxRawLinesInSummaryMode+1 {
		t.Errorf("summary = %+v, want %d lines summarized", summary, maxRawLinesInSummaryMode+1)
	}
}

func TestGetServiceLogs_DefaultLimitDependsOnResponseMode(t *testing.T) {
	lines := timeoutLines(defaultServiceLogsSummaryLimit + 1)
	for _, mode := range []string{"", "summary"} {
		t.Run("mode="+mode, func(t *testing.T) {
			out := callGetServiceLogs(t, lines, GetServiceLogsArgs{ResponseMode: mode})
			summary := decodeSummary(t, out)
			if summary.LinesSummarized != defaultServiceLogsSummaryLimit || !summary.Truncated {
				t.Errorf("summary = %+v, want %d lines summarized and truncated", summary, defaultServiceLogsSummaryLimit)
			}
		})
	}
	t.Run("mode=raw", func(t *testing.T) {
		out := callGetServiceLogs(t, lines, GetServiceLogsArgs{ResponseMode: "raw"})
		if logs, _ := out["logs"].([]any); len(logs) != defaultServiceLogsRawLimit {
			t.Errorf("got %d log entries, want %d", len(logs), defaultServiceLogsRawLimit)
		}
	})
}

func TestGetServiceLogs_RejectsUnknownResponseModeBeforeCalling(t *testing.T) {
	srv, requests := streamSearchServer(t, timeoutLines(30))
	_, _, err := NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:  "api",
		ResponseMode: "lines",
	})
	if err == nil || !strings.Contains(err.Error(), `response_mode must be "summary" or "raw", got "lines"`) {
		t.Errorf("err = %v", err)
	}
	if *requests != 0 {
		t.Errorf("%d upstream requests, want none", *requests)
	}
}

func TestGetServiceLogsInputSchema_DocumentsResponseMode(t *testing.T) {
	prop, _ := GetServiceLogsInputSchema()["properties"].(map[string]interface{})["response_mode"].(map[string]interface{})
	if prop == nil {
		t.Fatal("response_mode missing from the get_service_logs schema")
	}
	if types, _ := prop["type"].([]string); strings.Join(types, ",") != "string,null" {
		t.Errorf("type = %v, want string,null", prop["type"])
	}
	desc, _ := prop["description"].(string)
	for _, want := range []string{`"summary" (default`, `"raw"`, "over 20 lines"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description %q does not state %s", desc, want)
		}
	}
}
