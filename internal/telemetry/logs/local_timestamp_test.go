package logs

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProjectRawLogsUsesReadableTimes(t *testing.T) {
	result := streamsResult([]summaryTestLine{{severity: "INFO", message: "10:55:45.123 INFO txn=TXN-0001 created"}})
	projectLogTimes(timezoneRequest(), result)
	rows := result["rows"].([]logTimeRow)
	if rows[0].Time != "2023-11-15 03:43:20 IST" || rows[0].TimeUTC != "2023-11-14T22:13:20Z" {
		t.Fatalf("row times = %#v", rows[0])
	}
	if _, ok := result["data"]; ok {
		t.Fatal("projected result retained opaque data rows")
	}
	encoded, _ := json.Marshal(rows[0])
	if string(encoded[:8]) != `{"time":` {
		t.Fatalf("time is not first: %s", encoded)
	}
}

func TestProjectRawLogsPreservesLabelsAndNanoseconds(t *testing.T) {
	result := map[string]interface{}{"data": map[string]interface{}{"resultType": "streams", "result": []interface{}{
		map[string]interface{}{"stream": map[string]interface{}{"ServiceName": "api", "status_code": "500"}, "values": []interface{}{
			[]interface{}{"1699999999123456789", "failed"},
		}},
	}}}
	projectLogTimes(timezoneRequest(), result)
	row := result["rows"].([]logTimeRow)[0]
	if row.TimeUTC != "2023-11-14T22:13:19.123456789Z" {
		t.Fatalf("time_utc = %q", row.TimeUTC)
	}
	if row.Labels["ServiceName"] != "api" || row.Labels["status_code"] != "500" {
		t.Fatalf("labels = %#v", row.Labels)
	}
}

func TestProjectRawLogsWithoutValidZoneStillUsesReadableUTC(t *testing.T) {
	for _, req := range []*mcp.CallToolRequest{
		{},
		{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{"last9/client-timezone": "not/a-zone"}}},
	} {
		result := streamsResult([]summaryTestLine{{severity: "INFO", message: "served"}})
		projectLogTimes(req, result)
		row := result["rows"].([]logTimeRow)[0]
		if row.Time != "" || row.TimeUTC != "2023-11-14T22:13:20Z" {
			t.Fatalf("row = %#v", row)
		}
		if _, ok := result["l9_time"]; ok {
			t.Fatal("invalid or absent zone produced l9_time")
		}
	}
}

func TestProjectRawLogsRejectsServerLocalTimezone(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{clientTimezoneMetaKey: "Local"}}}
	result := streamsResult([]summaryTestLine{{severity: "INFO", message: "served"}})

	projectLogTimes(req, result)

	row := result["rows"].([]logTimeRow)[0]
	if row.Time != "" {
		t.Fatalf("local time = %q", row.Time)
	}
	if _, ok := result["l9_time"]; ok {
		t.Fatal("server-local zone produced l9_time")
	}
}

func TestBodyClockRequiresConsistentQuarterHourOffset(t *testing.T) {
	lines := make([]summaryTestLine, 16)
	for i := range lines {
		lines[i] = summaryTestLine{severity: "INFO", message: "02:13:20 INFO served"}
	}
	result := streamsResult(lines)
	projectLogTimes(timezoneRequest(), result)
	clock := result["l9_time"].(logTimeMetadata).BodyClock
	if clock == nil || clock.UTCOffset != "+04:00" || clock.RowsAgreeing != 16 || clock.RowsChecked != 16 {
		t.Fatalf("body clock = %#v", clock)
	}
	lines[0].message = "02:13:21 INFO disagrees"
	result = streamsResult(lines[:2])
	projectLogTimes(timezoneRequest(), result)
	if clock := result["l9_time"].(logTimeMetadata).BodyClock; clock != nil {
		t.Fatalf("mixed body clock = %#v", clock)
	}
}

func TestBodyClockOmitsMissingAndNonQuarterHourOffsets(t *testing.T) {
	for _, message := range []string{"no leading clock", "02:20:20 INFO seven-minute offset"} {
		lines := make([]summaryTestLine, 3)
		for i := range lines {
			lines[i] = summaryTestLine{severity: "INFO", message: message}
		}
		result := streamsResult(lines)
		projectLogTimes(timezoneRequest(), result)
		if clock := result["l9_time"].(logTimeMetadata).BodyClock; clock != nil {
			t.Fatalf("message %q produced body clock %#v", message, clock)
		}
	}
}

func TestProjectRawLogsFormatsEachSideOfDSTTransition(t *testing.T) {
	values := []interface{}{
		[]interface{}{fmt.Sprint(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).UnixNano()), "before"},
		[]interface{}{fmt.Sprint(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).UnixNano()), "after"},
	}
	result := map[string]interface{}{"data": map[string]interface{}{"resultType": "streams", "result": []interface{}{map[string]interface{}{"stream": map[string]interface{}{}, "values": values}}}}
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{clientTimezoneMetaKey: "America/New_York"}}}
	projectLogTimes(req, result)
	rows := result["rows"].([]logTimeRow)
	if rows[0].Time != "2026-11-01 01:30:00 EDT" || rows[1].Time != "2026-11-01 01:30:00 EST" {
		t.Fatalf("DST rows = %#v", rows)
	}
}

func TestProjectSummaryTimesIncludesSamples(t *testing.T) {
	result := streamsResult(timeoutLines(maxRawLinesInSummaryMode + 1))
	summarized := summarizeLogResult(result, summaryTestStartMs, summaryTestEndMs, false)
	projectSummaryTimes(timezoneRequest(), summarized)
	summary := summarized["summary"].(logSummary)
	if summary.FirstTimestamp == "" || summary.LastTimestamp == "" || len(summary.Patterns[0].Samples) == 0 {
		t.Fatalf("summary times missing: %#v", summary)
	}
	if got := summary.Patterns[0].Samples[0].Time; got == "" || got[len(got)-6:] != "+05:30" {
		t.Fatalf("sample time = %q", got)
	}
}
