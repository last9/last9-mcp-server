package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"last9-mcp/internal/models"
)

// HasCountAggregateStage reports whether pipeline contains an aggregate or
// window_aggregate stage whose function/aggregates include a $count.
func HasCountAggregateStage(pipeline []map[string]interface{}) bool {
	for _, stage := range pipeline {
		stageType, _ := stage["type"].(string)

		switch stageType {
		case "aggregate":
			aggregates, ok := stage["aggregates"].([]interface{})
			if !ok {
				continue
			}
			for _, rawAggregate := range aggregates {
				aggregate, ok := rawAggregate.(map[string]interface{})
				if !ok {
					continue
				}
				if isCountFunction(aggregate["function"]) {
					return true
				}
			}
		case "window_aggregate":
			if isCountFunction(stage["function"]) {
				return true
			}
		}
	}
	return false
}

func isCountFunction(rawFunction interface{}) bool {
	function, ok := rawFunction.(map[string]interface{})
	if !ok {
		return false
	}
	_, hasCount := function["$count"]
	return hasCount
}

// ExtractSingleServiceName scans a pipeline's filter stages (including inside
// $and/$or/$not) for $eq conditions on the ServiceName field. It returns the
// value and true only when exactly one distinct service name is present.
func ExtractSingleServiceName(pipeline []map[string]interface{}) (string, bool) {
	services := map[string]struct{}{}

	for _, stage := range pipeline {
		stageType, _ := stage["type"].(string)
		if stageType != "filter" {
			continue
		}
		collectServiceNameEquals(stage["query"], services)
	}

	if len(services) != 1 {
		return "", false
	}
	for service := range services {
		return service, true
	}
	return "", false
}

func collectServiceNameEquals(condition interface{}, out map[string]struct{}) {
	switch typed := condition.(type) {
	case map[string]interface{}:
		for key, value := range typed {
			switch key {
			case "$eq":
				args, ok := value.([]interface{})
				if !ok || len(args) != 2 {
					continue
				}
				field, ok := args[0].(string)
				if !ok || field != "ServiceName" {
					continue
				}
				if service, ok := args[1].(string); ok {
					out[service] = struct{}{}
				}
			case "$and", "$or", "$not":
				collectServiceNameEquals(value, out)
			}
		}
	case []interface{}:
		for _, item := range typed {
			collectServiceNameEquals(item, out)
		}
	}
}

// SumAggregateCount sums the count values across the groups/buckets of a log
// aggregate API response. Rows are shaped {"metric": {...}, "values": []} —
// "metric" holds the aggregate's `as`-aliased count(s) as JSON numbers
// alongside any group-by labels/`__ts__` as strings; "values" is unused here
// (that's a Prometheus instant-query shape, not this API's). Any numeric
// field in "metric" counts; group-by/`__ts__` strings are skipped. It returns
// false when no row yields a numeric field (guardrail must skip, not
// miscount, in that case).
func SumAggregateCount(response map[string]interface{}) (float64, bool) {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return 0, false
	}
	result, ok := data["result"].([]interface{})
	if !ok {
		return 0, false
	}
	if len(result) == 0 {
		return 0, true
	}

	var sum float64
	found := false
	for _, rawItem := range result {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		metric, ok := item["metric"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, value := range metric {
			switch value.(type) {
			case float64, json.Number:
				sum += promNumberFromAny(value)
				found = true
			}
		}
	}

	if !found {
		return 0, false
	}
	return sum, true
}

func promNumberFromAny(raw interface{}) float64 {
	switch value := raw.(type) {
	case float64:
		return value
	case json.Number:
		f, err := value.Float64()
		if err == nil {
			return f
		}
	case string:
		var f float64
		if _, err := fmt.Sscanf(value, "%g", &f); err == nil {
			return f
		}
	}
	return 0
}

type promInstantVectorPoint struct {
	Value []interface{} `json:"value"`
}

// AppendCountSanity attaches a top-level "l9_sanity" block to response
// comparing a $count aggregate's matched_count against the service's total
// log volume over the same window (physical_index_service_count). It is a
// pure guardrail add-on: on any failure to determine a single service, parse
// the matched count, or fetch/parse the baseline, it returns response
// unchanged. Never blocks or alters the underlying result.
func AppendCountSanity(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, startMs, endMs int64, response map[string]interface{}) map[string]interface{} {
	if !HasCountAggregateStage(pipeline) {
		return response
	}

	service, ok := ExtractSingleServiceName(pipeline)
	if !ok {
		return response
	}

	matchedCount, ok := SumAggregateCount(response)
	if !ok {
		return response
	}

	windowMinutes := (endMs - startMs) / 60000
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	promql := fmt.Sprintf(`sum(sum_over_time(physical_index_service_count{service_name=%q}[%dm]))`, service, windowMinutes)

	resp, err := MakePromInstantAPIQuery(ctx, client, promql, endMs/1000, cfg)
	if err != nil {
		return response
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return response
	}

	var instant []promInstantVectorPoint
	if err := json.NewDecoder(resp.Body).Decode(&instant); err != nil {
		return response
	}

	var volume float64
	found := false
	for _, point := range instant {
		if len(point.Value) != 2 {
			continue
		}
		volume += promNumberFromAny(point.Value[1])
		found = true
	}
	if !found || volume <= 0 {
		return response
	}

	ratio := math.Round((matchedCount/volume)*10000) / 10000

	note := ""
	if ratio > 0.05 {
		note = fmt.Sprintf(
			"matched count is %.2f%% of ALL log lines this service emitted in the window — if this was meant to count errors, the filter is likely too broad (e.g. matching a component/logger name without an ERROR gate); re-narrow and re-count.",
			ratio*100,
		)
	}

	response["l9_sanity"] = map[string]interface{}{
		"matched_count":      int64(math.Round(matchedCount)),
		"service_log_volume": volume,
		"ratio":              ratio,
		"note":               note,
	}

	return response
}
