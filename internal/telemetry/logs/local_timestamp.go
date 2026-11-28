package logs

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const clientTimezoneMetaKey = "last9/client-timezone"

var leadingBodyClock = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:\s|$)`)

type logTimeRow struct {
	Time     string `json:"time,omitempty"`
	TimeUTC  string `json:"time_utc"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message"`
}

type logTimeMetadata struct {
	Zone      string        `json:"zone"`
	Abbr      string        `json:"abbr"`
	UTCOffset string        `json:"utc_offset"`
	Use       string        `json:"use"`
	BodyClock *logBodyClock `json:"body_clock,omitempty"`
}

type logBodyClock struct {
	UTCOffset    string `json:"utc_offset"`
	RowsAgreeing int    `json:"rows_agreeing"`
	RowsChecked  int    `json:"rows_checked"`
	Note         string `json:"note"`
}

func requestLocation(req *mcp.CallToolRequest) (string, *time.Location) {
	if req == nil || req.Params == nil {
		return "", nil
	}
	name, ok := req.Params.Meta[clientTimezoneMetaKey].(string)
	if !ok || name == "" {
		return "", nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return "", nil
	}
	return name, loc
}

func projectLogTimes(req *mcp.CallToolRequest, result map[string]interface{}) {
	kind, _, err := extractResultItems(result)
	if err != nil || kind != "streams" {
		return
	}
	entries := parseServiceLogEntries(map[string]any{"data": result["data"]}, "")
	rows, metadata := projectEntries(req, entries)
	delete(result, "data")
	result["rows"] = rows
	if metadata != nil {
		result["l9_time"] = *metadata
	}
}

func projectEntries(req *mcp.CallToolRequest, entries []LogEntry) ([]logTimeRow, *logTimeMetadata) {
	zone, loc := requestLocation(req)
	rows := make([]logTimeRow, 0, len(entries))
	for _, entry := range entries {
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		row := logTimeRow{TimeUTC: ts.UTC().Format(time.RFC3339Nano), Severity: entry.Severity, Message: entry.Message}
		if loc != nil {
			row.Time = ts.In(loc).Format("2006-01-02 15:04:05 MST")
		}
		rows = append(rows, row)
	}
	if loc != nil && len(rows) > 0 {
		ts, _ := time.Parse(time.RFC3339Nano, rows[0].TimeUTC)
		abbr, offset := ts.In(loc).Zone()
		metadata := &logTimeMetadata{Zone: zone, Abbr: abbr, UTCOffset: formatUTCOffset(offset), Use: "time", BodyClock: inferBodyClock(rows, offset, abbr)}
		return rows, metadata
	}
	return rows, nil
}

func projectSummaryTimes(req *mcp.CallToolRequest, result map[string]interface{}) {
	summary, ok := result["summary"].(logSummary)
	if !ok {
		return
	}
	_, loc := requestLocation(req)
	project := func(value string) string {
		ts, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return value
		}
		if loc == nil {
			return ts.UTC().Format(time.RFC3339Nano)
		}
		return ts.In(loc).Format("2006-01-02 15:04:05 MST")
	}
	summary.FirstTimestamp = project(summary.FirstTimestamp)
	summary.LastTimestamp = project(summary.LastTimestamp)
	for i := range summary.Patterns {
		for j := range summary.Patterns[i].Samples {
			summary.Patterns[i].Samples[j].Time = project(summary.Patterns[i].Samples[j].Time)
		}
	}
	result["summary"] = summary
}

func inferBodyClock(rows []logTimeRow, userOffset int, userAbbr string) *logBodyClock {
	counts := map[int]int{}
	checked := 0
	for _, row := range rows {
		match := leadingBodyClock.FindStringSubmatch(row.Message)
		if match == nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, row.TimeUTC)
		if err != nil {
			continue
		}
		hour, _ := strconv.Atoi(match[1])
		minute, _ := strconv.Atoi(match[2])
		second, _ := strconv.Atoi(match[3])
		if hour > 23 || minute > 59 || second > 59 {
			continue
		}
		offset := hour*3600 + minute*60 + second - (ts.Hour()*3600 + ts.Minute()*60 + ts.Second())
		for offset < -12*3600 {
			offset += 24 * 3600
		}
		for offset > 14*3600 {
			offset -= 24 * 3600
		}
		checked++
		counts[offset]++
	}
	bestOffset, agreeing := 0, 0
	for offset, count := range counts {
		if count > agreeing {
			bestOffset, agreeing = offset, count
		}
	}
	if checked < 3 || agreeing*10 < checked*9 || bestOffset%(15*60) != 0 {
		return nil
	}
	delta := userOffset - bestOffset
	relation := fmt.Sprintf("%s behind `time`", formatDuration(absInt(delta)))
	if delta < 0 {
		relation = fmt.Sprintf("%s ahead of `time`", formatDuration(absInt(delta)))
	} else if delta == 0 {
		relation = "the same offset as `time`"
	}
	comparison := fmt.Sprintf("It is %s.", relation)
	if bestOffset != 0 && bestOffset != userOffset {
		comparison = fmt.Sprintf("It is %s. It is not UTC and not %s.", relation, userAbbr)
	}
	return &logBodyClock{UTCOffset: formatUTCOffset(bestOffset), RowsAgreeing: agreeing, RowsChecked: checked,
		Note: fmt.Sprintf("Clock strings inside message text are the emitting service's own clock (%s). %s", formatUTCOffset(bestOffset), comparison)}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func formatUTCOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	return fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, seconds%3600/60)
}

func formatDuration(seconds int) string {
	return fmt.Sprintf("%dh%02dm", seconds/3600, seconds%3600/60)
}
