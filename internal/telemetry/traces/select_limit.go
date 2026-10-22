package traces

import (
	"fmt"
	"math"
)

// The query engine applies a final select's own limit only when it is
// positive and ignores the request limit for it, so a final select must carry
// a bounded limit or it returns every span in the window.
const (
	maxSelectLimit  = 1000
	maxSelectOffset = 10000
)

// validateFinalSelectBounds rejects an explicit limit/offset on the last
// select stage that is not a whole number in range. A missing limit is
// filled in by boundFinalSelectLimit before the request is sent.
func validateFinalSelectBounds(stage map[string]interface{}, path string) error {
	if raw, ok := stage["limit"]; ok {
		if n, whole := wholeNumber(raw); !whole || n < 1 || n > maxSelectLimit {
			return &tracePipelineError{
				category: traceCategoryInvalidField,
				path:     path + ".limit",
				msg:      fmt.Sprintf(`"limit" at %s.limit must be an integer from 1 to %d, e.g. {"type":"select","order":["Duration"],"limit":10}; got %v`, path, maxSelectLimit, raw),
			}
		}
	}
	if raw, ok := stage["offset"]; ok {
		if n, whole := wholeNumber(raw); !whole || n < 0 || n > maxSelectOffset {
			return &tracePipelineError{
				category: traceCategoryInvalidField,
				path:     path + ".offset",
				msg:      fmt.Sprintf(`"offset" at %s.offset must be an integer from 0 to %d; got %v`, path, maxSelectOffset, raw),
			}
		}
	}
	return nil
}

// boundFinalSelectLimit sets the request limit, capped at maxSelectLimit, on a
// final select stage that has none.
func boundFinalSelectLimit(stages []map[string]interface{}, requestLimit int) {
	if len(stages) == 0 {
		return
	}
	last := stages[len(stages)-1]
	if stageType, _ := last["type"].(string); stageType != "select" {
		return
	}
	if _, ok := last["limit"]; ok {
		return
	}
	last["limit"] = min(requestLimit, maxSelectLimit)
}

func wholeNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return int(n), true
		}
	}
	return 0, false
}
