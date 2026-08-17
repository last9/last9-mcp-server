package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"last9-mcp/internal/constants"
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

// ExtractCountAliases returns the `as` alias(es) for all $count aggregates in
// pipeline aggregate/window_aggregate stages.
func ExtractCountAliases(pipeline []map[string]interface{}) map[string]struct{} {
	out := map[string]struct{}{}
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
				if !isCountFunction(aggregate["function"]) {
					continue
				}
				if as, ok := aggregate["as"].(string); ok && as != "" {
					out[as] = struct{}{}
				}
			}
		case "window_aggregate":
			if !isCountFunction(stage["function"]) {
				continue
			}
			if as, ok := stage["as"].(string); ok && as != "" {
				out[as] = struct{}{}
			}
		}
	}
	return out
}

// ExtractSingleServiceName scans a pipeline's filter stages (including inside
// $and/$or) for $eq conditions on the ServiceName field. It returns the value
// and true only when exactly one distinct service name is present.
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
			case "$and", "$or":
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
func SumAggregateCount(response map[string]interface{}, countAliases map[string]struct{}) (float64, bool) {
	if len(countAliases) == 0 {
		return 0, false
	}
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return 0, false
	}
	result, ok := data["result"].([]interface{})
	if !ok {
		return 0, false
	}
	if len(result) == 0 {
		return 0, false
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
		for k, value := range metric {
			if _, ok := countAliases[k]; !ok {
				continue
			}
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
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return 0
}

type promInstantVectorPoint struct {
	Value []interface{} `json:"value"`
}

// resultRowsEmpty reports whether response's data.result is present AND
// genuinely empty (len == 0). It returns false — "inconclusive", not "empty"
// — when data/result is missing or the wrong shape, so callers can't
// misinterpret a malformed response as a real zero-result shape.
func resultRowsEmpty(response map[string]interface{}) bool {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return false
	}
	result, ok := data["result"].([]interface{})
	if !ok {
		return false
	}
	return len(result) == 0
}

// zeroCountSanityBlock builds the l9_sanity block emitted when a $count
// aggregate over a pipeline with a parse stage yields zero matches. There is
// nothing to compare a zero against, so no PromQL baseline fetch happens for
// this block.
func zeroCountSanityBlock() map[string]interface{} {
	return map[string]interface{}{
		"matched_count": int64(0),
		"note": "matched_count is 0 and the pipeline has a parse stage — this is either a genuine zero (no matching rows in this window) or the parse stage not matching the Body's real format; call get_log_attributes_for_pipeline and inspect sample_body to confirm the actual shape before assuming either explanation. Note an unanchored regexp capture group can also match the wrong token (e.g. a leading timestamp) and return a confidently wrong nonzero count instead.",
	}
}

// AppendCountSanity attaches a top-level "l9_sanity" block to response
// comparing a $count aggregate's matched_count against the service's total
// log volume over the same window (physical_index_service_count). It also
// covers a zero-count-with-parse-stage case (see zeroCountSanityBlock): a
// zero match count when the pipeline has a parse stage gets the same
// self-correcting note without a PromQL baseline fetch, since there is
// nothing to compare a zero against. It is a pure guardrail add-on: on any
// other failure to determine a single service, parse the matched count, or
// fetch/parse the baseline, it returns response unchanged. Never blocks or
// alters the underlying result.
func AppendCountSanity(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, startMs, endMs int64, response map[string]interface{}) map[string]interface{} {
	if !HasCountAggregateStage(pipeline) {
		return response
	}

	countAliases := ExtractCountAliases(pipeline)
	if len(countAliases) == 0 {
		return response
	}

	service, ok := ExtractSingleServiceName(pipeline)
	if !ok {
		return response
	}

	matchedCount, ok := SumAggregateCount(response, countAliases)
	if !ok {
		// SumAggregateCount also returns !ok when data.result is genuinely
		// empty (len == 0) — the shape a groupby'd $count with zero matches
		// actually takes (as opposed to a single zero-valued row). That's
		// just as suspicious as an explicit zero when the pipeline has a
		// parse stage, so flag it the same way. CRITICAL: only for a
		// genuinely empty result set — !ok also occurs when rows exist but
		// no count alias could be parsed, which must stay untouched.
		if hasParseStage(pipeline) && resultRowsEmpty(response) {
			response["l9_sanity"] = zeroCountSanityBlock()
			return response
		}
		return response
	}
	if matchedCount <= 0 {
		// A zero count is the most suspicious outcome of all — silently
		// returning no l9_sanity block here is exactly the "confidently
		// wrong/silent zero" failure this guardrail exists to catch. When
		// the pipeline has a parse stage, the most likely explanation is
		// that the parser doesn't match the Body's real shape (e.g. a JSON
		// parser against a plaintext Body), so flag it — but there is
		// nothing to compare a zero against, so skip the PromQL baseline
		// fetch entirely. Without a parse stage in the pipeline (a plain
		// filter matching zero rows), a true zero is unremarkable — leave
		// response untouched exactly as before.
		if !hasParseStage(pipeline) {
			return response
		}
		response["l9_sanity"] = zeroCountSanityBlock()
		return response
	}

	windowMinutes := (endMs - startMs) / 60000
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	promql := fmt.Sprintf(`sum(sum_over_time(physical_index_service_count{service_name=%q}[%dm]))`, service, windowMinutes)

	instantCtx, cancel := context.WithTimeout(ctx, constants.PerChunkHTTPTimeout)
	defer cancel()
	resp, err := MakePromInstantAPIQuery(instantCtx, client, promql, endMs/1000, cfg)
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
