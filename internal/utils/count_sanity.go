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
// aggregate over a pipeline that parses or filters Body yields zero matches
// and the volume baseline could not be used to disambiguate (service name
// not extractable, or the baseline fetch failed) — the ambiguous fallback
// note, unchanged from before the service_log_volume discriminator existed.
func zeroCountSanityBlock() map[string]interface{} {
	return map[string]interface{}{
		"matched_count": int64(0),
		"note": "matched_count is 0 and the pipeline parses or filters Body — this is either a genuine zero (no matching rows in this window) or the parse stage / Body regex-or-contains not matching the Body's real format; call get_log_attributes_for_pipeline and inspect sample_bodies to confirm the actual shape before assuming either explanation. Note an unanchored regexp capture group can also match the wrong token (e.g. a leading timestamp) and return a confidently wrong nonzero count instead.",
	}
}

// serviceVolumeBaseline fetches the physical_index_service_count PromQL
// baseline for service over a window of windowMinutes ending at endMs. It is
// shared by the nonzero ratio path and the zero-path genuine-zero check.
// queryOK is true whenever the query executed and returned a well-formed
// instant-vector response — INCLUDING an empty series list, which is a
// legitimate answer meaning the service has zero log volume (a service that
// emitted nothing has no series in physical_index_service_count at all, so
// the PromQL response is empty rather than carrying an explicit 0 sample).
// queryOK is false only for a genuine failure to get an answer: HTTP error
// status, transport error, or a decode failure — callers must treat that as
// "could not determine", never as a volume of zero.
func serviceVolumeBaseline(ctx context.Context, client *http.Client, cfg models.Config, service string, endMs int64, windowMinutes int64) (volume float64, queryOK bool) {
	promql := fmt.Sprintf(`sum(sum_over_time(physical_index_service_count{service_name=%q}[%dm]))`, service, windowMinutes)

	instantCtx, cancel := context.WithTimeout(ctx, constants.PerChunkHTTPTimeout)
	defer cancel()
	resp, err := MakePromInstantAPIQuery(instantCtx, client, promql, endMs/1000, cfg)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, false
	}

	var instant []promInstantVectorPoint
	if err := json.NewDecoder(resp.Body).Decode(&instant); err != nil {
		return 0, false
	}

	for _, point := range instant {
		if len(point.Value) != 2 {
			continue
		}
		volume += promNumberFromAny(point.Value[1])
	}
	// An empty (or all-malformed-point) series list is still a well-formed
	// "zero volume" answer, not a failure — queryOK is true regardless of
	// whether the loop above added anything.
	return volume, true
}

// zeroCountSanityBlockWithBaseline builds the l9_sanity block for a zero
// count (or empty-result) match on a pipeline that parses or filters Body.
// It tries to disambiguate a genuine zero (the service emitted no logs in
// this window at all) from a parse/filter mismatch (logs existed but nothing
// matched) using the same physical_index_service_count baseline the nonzero
// path uses. Falls back to the ambiguous zeroCountSanityBlock note when the
// service name can't be extracted to a single value or the baseline fetch
// fails for any reason — this must never block or fail the tool.
func zeroCountSanityBlockWithBaseline(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, startMs, endMs int64) map[string]interface{} {
	service, ok := ExtractSingleServiceName(pipeline)
	if !ok {
		return zeroCountSanityBlock()
	}

	windowMinutes := (endMs - startMs) / 60000
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	volume, queryOK := serviceVolumeBaseline(ctx, client, cfg, service, endMs, windowMinutes)
	if !queryOK {
		return zeroCountSanityBlock()
	}

	if volume == 0 {
		return map[string]interface{}{
			"matched_count":      int64(0),
			"service_log_volume": float64(0),
			"note":               "matched_count is 0 and service_log_volume is 0 — the service emitted no logs at all in this window. This is a genuine zero, not a parse/filter mismatch; there is nothing to inspect via get_log_attributes_for_pipeline for this window.",
		}
	}

	return map[string]interface{}{
		"matched_count":      int64(0),
		"service_log_volume": volume,
		"note":               "matched_count is 0 but service_log_volume shows the service emitted logs in this window — the pipeline parses or filters Body and the parse stage / Body regex-or-contains is likely not matching the Body's real format; call get_log_attributes_for_pipeline and inspect sample_bodies to confirm the actual shape. Note an unanchored regexp capture group can also match the wrong token (e.g. a leading timestamp) and return a confidently wrong nonzero count instead.",
	}
}

// AppendCountSanity attaches a top-level "l9_sanity" block to response
// comparing a $count aggregate's matched_count against the service's total
// log volume over the same window (physical_index_service_count). It also
// covers a zero-count case where the pipeline parses or filters Body (see
// zeroCountSanityBlockWithBaseline): a zero match count attempts the same
// service_log_volume baseline to tell a genuine zero (service emitted no
// logs at all) apart from a parse/filter mismatch (logs existed but nothing
// matched), falling back to the ambiguous zeroCountSanityBlock note when the
// service can't be resolved to a single value or the baseline fetch fails.
// On any other failure to determine a single service (nonzero path only),
// parse the matched count, or fetch/parse the baseline, it returns response
// unchanged. Never blocks or alters the underlying result.
func AppendCountSanity(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, startMs, endMs int64, response map[string]interface{}) map[string]interface{} {
	if !HasCountAggregateStage(pipeline) {
		return response
	}

	countAliases := ExtractCountAliases(pipeline)
	if len(countAliases) == 0 {
		return response
	}

	touchesBody := hasParseStage(pipeline) || pipelineTouchesBody(pipeline)

	matchedCount, ok := SumAggregateCount(response, countAliases)
	if !ok {
		// SumAggregateCount also returns !ok when data.result is genuinely
		// empty (len == 0) — the shape a groupby'd $count with zero matches
		// actually takes (as opposed to a single zero-valued row). That's
		// just as suspicious as an explicit zero when the pipeline parses or
		// filters Body, so flag it the same way. CRITICAL: only for a
		// genuinely empty result set — !ok also occurs when rows exist but
		// no count alias could be parsed, which must stay untouched.
		if touchesBody && resultRowsEmpty(response) {
			response["l9_sanity"] = zeroCountSanityBlockWithBaseline(ctx, client, cfg, pipeline, startMs, endMs)
			return response
		}
		return response
	}
	if matchedCount <= 0 {
		// A zero count is the most suspicious outcome of all — silently
		// returning no l9_sanity block here is exactly the "confidently
		// wrong/silent zero" failure this guardrail exists to catch. When
		// the pipeline parses or filters Body, the most likely explanation is
		// that the parser/regex doesn't match the Body's real shape (e.g. a
		// JSON parser against a plaintext Body) — but it could also be a
		// genuine zero, so attempt the same service_log_volume baseline the
		// nonzero path uses to disambiguate (see
		// zeroCountSanityBlockWithBaseline). Without any Body involvement (a
		// plain filter on indexed fields matching zero rows), a true zero is
		// unremarkable — leave response untouched exactly as before.
		if !touchesBody {
			return response
		}
		response["l9_sanity"] = zeroCountSanityBlockWithBaseline(ctx, client, cfg, pipeline, startMs, endMs)
		return response
	}

	// Nonzero path only, from here: needs a single scoped service to fetch a
	// PromQL baseline for the ratio. The zero paths above don't need a
	// resolved service to proceed — zeroCountSanityBlockWithBaseline falls
	// back to the ambiguous note when it can't extract one.
	service, ok := ExtractSingleServiceName(pipeline)
	if !ok {
		return response
	}

	windowMinutes := (endMs - startMs) / 60000
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	volume, queryOK := serviceVolumeBaseline(ctx, client, cfg, service, endMs, windowMinutes)
	if !queryOK || volume <= 0 {
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
