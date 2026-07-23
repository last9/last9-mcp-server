package utils

import "strings"

// Port of the body-parse / JSON-parse detection helpers from the frontend
// dashboard (scenes/Logs/utils.tsx). Names and structure mirror the frontend
// 1:1 so behaviour parity is easy to audit.
//
// Each MCP logjson_query stage is a plain JSON object like:
//
//	{"type": "filter", "query": {"$contains": ["Body", "timeout"]}}
//	{"type": "parse",  "parser": "json", "field": "Body", "labels": [...]}
//	{"type": "transform", "transforms": [{"function": {"$split_into": ["Body", "msg"]}}]}
//
// The frontend operates on a typed `Pipeline` union; here we walk the raw
// JSON maps. Two deliberate simplifications versus the dashboard:
//
//  1. We treat `$eq` / `$ieq` on Body as the only "optimized" (indexable)
//     operators. The dashboard's full check is bloom-eligibility with a
//     per-operator token-count threshold — that's tied to the dashboard's
//     builder UI semantics and doesn't translate cleanly to agent queries.
//  2. We only count transforms whose function is in BODY_TRANSFORM_OPERATIONS
//     (matches the frontend list exactly: $split_into / $split / $replace_regex).

// PipelineHasAggregateStage reports whether any stage in the pipeline is an
// "aggregate" or "window_aggregate" stage. Chunking a pipeline that aggregates
// (group-by, avg/median/quantile/stddev, etc.) and concatenating the per-chunk
// results produces duplicate group-by keys and mathematically wrong
// aggregates, so callers must treat true here as "run as a single request,
// never chunk". Nil-safe.
func PipelineHasAggregateStage(pipeline []map[string]interface{}) bool {
	for _, stage := range pipeline {
		switch stageType, _ := stage["type"].(string); stageType {
		case "aggregate", "window_aggregate":
			return true
		}
	}
	return false
}

// HasJSONParsePipeline mirrors frontend hasJSONParsePipeline in
// adaptive-chunk-loader.ts:31 — true if any stage is a JSON parse.
func HasJSONParsePipeline(pipeline []map[string]any) bool {
	for _, stage := range pipeline {
		if stageType, _ := stage["type"].(string); stageType != "parse" {
			continue
		}
		if parser, _ := stage["parser"].(string); parser == "json" {
			return true
		}
	}
	return false
}

// HasBodyParseOrTransform mirrors frontend hasBodyParseOrTransform in
// utils.tsx:1765 — true if any parse or transform stage operates on the
// Body field.
func HasBodyParseOrTransform(pipeline []map[string]any) bool {
	for _, stage := range pipeline {
		stageType, _ := stage["type"].(string)
		switch stageType {
		case "parse":
			if isBodyField(stage["field"]) {
				return true
			}
		case "transform":
			if transformsTouchBody(stage["transforms"]) {
				return true
			}
		}
	}
	return false
}

// HasExpensiveBodyParsing mirrors frontend hasExpensiveBodyParsingInPipelines
// in utils.tsx:1858 — returns true when the pipeline does body work AND that
// work isn't already optimized (indexable).
func HasExpensiveBodyParsing(pipeline []map[string]any) bool {
	if HasBodyParseOrTransform(pipeline) {
		return true
	}

	// If no filter stage references Body at all, it's not expensive.
	if !anyStageFiltersOnBody(pipeline) {
		return false
	}

	// If ANY filter stage's body filter is optimized (indexable), the whole
	// query is considered optimized — skip aggressive chunking.
	for _, stage := range pipeline {
		if stageType, _ := stage["type"].(string); stageType != "filter" {
			continue
		}
		if isOptimizedBodyFilterStage(stage) {
			return false
		}
	}

	return true
}

// --- private helpers (lowercase to mirror the frontend's module-scoped fns) ---

func isBodyField(raw any) bool {
	field, _ := raw.(string)
	return strings.EqualFold(strings.TrimSpace(field), "body")
}

// bodyTransformOperations mirrors frontend BODY_TRANSFORM_OPERATIONS
// (utils.tsx:1763). Only these transform functions count as "body work" for
// the purpose of expensive-body-parsing detection.
var bodyTransformOperations = map[string]struct{}{
	"$split_into":    {},
	"$split":         {},
	"$replace_regex": {},
}

// transformsTouchBody returns true if any transform function in the listed
// operations has Body as its first argument. Mirrors the inner loop of
// frontend hasBodyParseOrTransform (utils.tsx:1777-1785).
func transformsTouchBody(raw any) bool {
	transforms, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, t := range transforms {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			continue
		}
		for op, args := range fn {
			if _, listed := bodyTransformOperations[op]; !listed {
				continue
			}
			argList, ok := args.([]any)
			if !ok || len(argList) == 0 {
				continue
			}
			if isBodyField(argList[0]) {
				return true
			}
		}
	}
	return false
}

// anyStageFiltersOnBody walks all filter stages and reports whether any
// $-operator condition references the Body field. Mirrors the union of
// getBodyFiltersInPipeline (basic + advanced) — simplified because every
// MCP filter stage is "advanced" (free-form JSON conditions).
func anyStageFiltersOnBody(pipeline []map[string]any) bool {
	for _, stage := range pipeline {
		if stageType, _ := stage["type"].(string); stageType != "filter" {
			continue
		}
		if stageHasBodyCondition(stage) {
			return true
		}
	}
	return false
}

func stageHasBodyCondition(stage map[string]any) bool {
	query, ok := stage["query"].(map[string]any)
	if !ok {
		return false
	}
	return conditionReferencesBody(query)
}

// conditionReferencesBody walks a $and / $or / $not tree and returns true if
// any leaf operator's first argument is the Body field. Matches the recursive
// extractor in frontend getBodyFiltersInPipeline (utils.tsx:1899).
func conditionReferencesBody(condition map[string]any) bool {
	for op, raw := range condition {
		switch op {
		case "$and", "$or", "$not":
			group, ok := raw.([]any)
			if !ok {
				continue
			}
			for _, c := range group {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if conditionReferencesBody(cm) {
					return true
				}
			}
		default:
			args, ok := raw.([]any)
			if !ok || len(args) == 0 {
				continue
			}
			if isBodyField(args[0]) {
				return true
			}
		}
	}
	return false
}

// isOptimizedBodyFilterStage mirrors frontend isFilterStageOptimized
// (utils.tsx:1842): a stage is optimized if ANY operator group on the Body
// field is index-eligible. Frontend uses a bloom-eligibility heuristic
// (BLOOM_FILTER_OPERATORS + token-count threshold for $contains etc.); this
// port simplifies to "is the operator indexable" via isIndexableBodyOperator
// because tokenized bloom heuristics are dashboard-specific.
func isOptimizedBodyFilterStage(stage map[string]any) bool {
	query, ok := stage["query"].(map[string]any)
	if !ok {
		return false
	}
	conds := collectBodyConditions(query)
	if len(conds) == 0 {
		return false
	}
	// Any indexable operator on Body makes the stage optimized — matches the
	// frontend's "any operator group bloom-eligible" semantics.
	for op := range conds {
		if isIndexableBodyOperator(op) {
			return true
		}
	}
	return false
}

func isIndexableBodyOperator(op string) bool {
	switch op {
	case "$eq", "$ieq":
		return true
	default:
		return false
	}
}

// collectBodyConditions returns the set of operator names that appear at
// body-touching leaves in the condition tree. Used by
// isOptimizedBodyFilterStage to decide whether at least one body-touching
// operator is indexable — matching the frontend's "any operator group
// bloom-eligible" semantics.
func collectBodyConditions(condition map[string]any) map[string]struct{} {
	ops := make(map[string]struct{})
	collectBodyConditionsInto(condition, ops)
	return ops
}

func collectBodyConditionsInto(condition map[string]any, out map[string]struct{}) {
	for op, raw := range condition {
		switch op {
		case "$and", "$or", "$not":
			group, ok := raw.([]any)
			if !ok {
				continue
			}
			for _, c := range group {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				collectBodyConditionsInto(cm, out)
			}
		default:
			args, ok := raw.([]any)
			if !ok || len(args) == 0 {
				continue
			}
			if isBodyField(args[0]) {
				out[op] = struct{}{}
			}
		}
	}
}
