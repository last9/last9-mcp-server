package logs

import (
	"fmt"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const clientTimezoneMetaKey = "last9/client-timezone"

func requestLocation(req *mcp.CallToolRequest) *time.Location {
	if req == nil || req.Params == nil {
		return nil
	}
	name, ok := req.Params.Meta[clientTimezoneMetaKey].(string)
	if !ok || name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

func formatLocalTimestamp(ts time.Time, loc *time.Location) string {
	return ts.In(loc).Format("2006-01-02 15:04:05 MST")
}

func addLocalTimestamps(req *mcp.CallToolRequest, result map[string]interface{}) {
	loc := requestLocation(req)
	if loc == nil {
		return
	}
	kind, items, err := extractResultItems(result)
	if err != nil || kind != "streams" {
		return
	}
	for _, item := range items {
		stream, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		values, _ := stream["values"].([]interface{})
		for i, value := range values {
			row, ok := value.([]interface{})
			if !ok || len(row) < 2 {
				continue
			}
			nanos, err := strconv.ParseInt(fmt.Sprint(row[0]), 10, 64)
			if err != nil {
				continue
			}
			row = append(row, map[string]interface{}{"ts_local": formatLocalTimestamp(time.Unix(0, nanos), loc)})
			values[i] = row
		}
	}
}

func addServiceLogLocalTimestamps(req *mcp.CallToolRequest, logs *ServiceLogsResponse) {
	loc := requestLocation(req)
	if loc == nil {
		return
	}
	for i := range logs.Logs {
		ts, err := time.Parse(time.RFC3339Nano, logs.Logs[i].Timestamp)
		if err == nil {
			logs.Logs[i].TSLocal = formatLocalTimestamp(ts, loc)
		}
	}
}
