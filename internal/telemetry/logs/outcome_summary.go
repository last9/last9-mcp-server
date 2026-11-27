package logs

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/models"
)

var outcomeFieldNames = []string{"status", "status_code", "elb_status_code", "backend_status_code"}

func recognizedOutcomeField(result map[string]interface{}) string {
	kind, items, err := extractResultItems(result)
	if err != nil || kind != "streams" {
		return ""
	}
	for _, item := range items {
		stream, _ := item.(map[string]interface{})
		labels, _ := stream["stream"].(map[string]interface{})
		for _, wanted := range outcomeFieldNames {
			for field := range labels {
				name := field
				if i := strings.LastIndexAny(name, ".'"); i >= 0 {
					name = strings.Trim(name[i+1:], "']")
				}
				if name == wanted {
					return field
				}
			}
		}
	}
	return ""
}

func outcomeAggregatePipeline(query []map[string]interface{}, field string) []map[string]interface{} {
	result := append([]map[string]interface{}{}, query...)
	return append(result, map[string]interface{}{
		"type": "aggregate",
		"aggregates": []interface{}{map[string]interface{}{
			"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count",
		}},
		"groupby": map[string]interface{}{field: "outcome"},
	})
}

func outcomeCounts(result map[string]interface{}) map[string]int64 {
	_, items, err := extractResultItems(result)
	if err != nil {
		return nil
	}
	counts := map[string]int64{}
	for _, item := range items {
		row, _ := item.(map[string]interface{})
		values, _ := row["stream"].(map[string]interface{})
		outcome := fmt.Sprint(values["outcome"])
		count, err := strconv.ParseInt(fmt.Sprint(values["count"]), 10, 64)
		if err == nil && outcome != "<nil>" {
			counts[outcome] = count
		}
	}
	return counts
}

type outcomeSummaryRequest struct {
	query          []map[string]interface{}
	field, index   string
	startMs, endMs int64
	result         map[string]interface{}
}

func appendOutcomeSummary(ctx context.Context, client *http.Client, cfg models.Config, request outcomeSummaryRequest) {
	if request.field == "" {
		request.field = recognizedOutcomeField(request.result)
	}
	if request.field == "" {
		return
	}
	aggregate, err := executeLogJSONQuery(ctx, client, cfg, outcomeAggregatePipeline(request.query, request.field), request.startMs, request.endMs, 100, request.index)
	if err != nil {
		return
	}
	counts := outcomeCounts(aggregate)
	if len(counts) == 0 {
		return
	}
	request.result["l9_outcome_summary"] = map[string]interface{}{
		"field": request.field, "counts": counts,
		"window": []string{time.UnixMilli(request.startMs).UTC().Format(time.RFC3339Nano), time.UnixMilli(request.endMs).UTC().Format(time.RFC3339Nano)},
	}
}
