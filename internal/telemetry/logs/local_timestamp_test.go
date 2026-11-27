package logs

import (
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAddLocalTimestampsUsesClientTimezone(t *testing.T) {
	result := streamsResult([]summaryTestLine{{severity: "INFO", message: "served"}})
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{"last9/client-timezone": "Asia/Kolkata"}}}

	addLocalTimestamps(req, result)

	values := result["data"].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["values"].([]interface{})
	row := values[0].([]interface{})
	meta := row[2].(map[string]interface{})
	want := time.UnixMilli(summaryTestStartMs).In(time.FixedZone("IST", 5*60*60+30*60)).Format("2006-01-02 15:04:05 MST")
	if meta["ts_local"] != want {
		t.Fatalf("ts_local = %v, want %q", meta["ts_local"], want)
	}
}

func TestAddLocalTimestampsIgnoresMissingAndUnknownTimezone(t *testing.T) {
	for _, req := range []*mcp.CallToolRequest{
		{},
		{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{"last9/client-timezone": "not/a-zone"}}},
	} {
		result := streamsResult([]summaryTestLine{{severity: "INFO", message: "served"}})
		addLocalTimestamps(req, result)
		values := result["data"].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["values"].([]interface{})
		if got := len(values[0].([]interface{})); got != 2 {
			t.Fatalf("row length = %d, want 2", got)
		}
	}
}

func TestAddServiceLogLocalTimestamps(t *testing.T) {
	logs := &ServiceLogsResponse{Logs: []LogEntry{{Timestamp: "2026-11-09T06:55:45Z"}}}
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{"last9/client-timezone": "Asia/Kolkata"}}}

	addServiceLogLocalTimestamps(req, logs)

	if got := logs.Logs[0].TSLocal; got != "2026-11-09 12:25:45 IST" {
		t.Fatalf("ts_local = %q", got)
	}
}
