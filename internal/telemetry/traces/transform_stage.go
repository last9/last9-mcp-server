package traces

import (
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
