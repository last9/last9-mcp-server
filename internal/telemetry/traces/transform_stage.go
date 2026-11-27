package traces

import (
	"encoding/json"
	"fmt"
	"strings"
)

const traceCategoryTransformShape = "transform_shape"

const transformShapeExample = `{"type":"transform","transforms":[{"function":{"$split":["SpanName","/",1]},"as":["endpoint"]}]}`

// normalizeTransformStage rewrites a string "as" into the one-element array the
// API requires: a bare string is the most common rejected trace pipeline.
func normalizeTransformStage(stage map[string]interface{}, path string) error {
	transforms, ok := stage["transforms"].([]interface{})
	if !ok {
		return nil
	}
	for i, raw := range transforms {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if err := validateSplitIndex(entry["function"], fmt.Sprintf("%s.transforms[%d].function", path, i)); err != nil {
			return err
		}
		asPath := fmt.Sprintf("%s.transforms[%d].as", path, i)
		if name, ok := entry["as"].(string); ok && strings.TrimSpace(name) != "" {
			entry["as"] = []interface{}{name}
			continue
		}
		if !isNonEmptyNameList(entry["as"]) {
			return &tracePipelineError{
				category: traceCategoryTransformShape,
				path:     asPath,
				msg:      `transform "as" must be a non-empty array of output field names, e.g. ` + transformShapeExample,
			}
		}
	}
	return nil
}

// validateSplitIndex rejects a zero $split index: indexes are one-based, so 0
// selects no element and every row silently gets an empty value.
func validateSplitIndex(function interface{}, path string) error {
	fn, ok := function.(map[string]interface{})
	if !ok {
		return nil
	}
	operands, ok := fn["$split"].([]interface{})
	if !ok || len(operands) < 3 || !isZeroNumber(operands[2]) {
		return nil
	}
	return &tracePipelineError{
		category: traceCategoryTransformShape,
		path:     path + ".$split[2]",
		msg:      `$split index is one-based; use 1 for the first element, e.g. ` + transformShapeExample,
	}
}

func isZeroNumber(value interface{}) bool {
	switch n := value.(type) {
	case float64:
		return n == 0
	case int:
		return n == 0
	case json.Number:
		f, err := n.Float64()
		return err == nil && f == 0
	}
	return false
}

func isNonEmptyNameList(value interface{}) bool {
	names, ok := value.([]interface{})
	if !ok || len(names) == 0 {
		return false
	}
	for _, raw := range names {
		if name, ok := raw.(string); !ok || strings.TrimSpace(name) == "" {
			return false
		}
	}
	return true
}
