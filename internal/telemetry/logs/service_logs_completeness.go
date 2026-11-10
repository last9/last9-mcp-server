package logs

import (
	"encoding/json"
	"fmt"
	"math"
)

// chunksFailed reads search_stats.chunks_failed, absent meaning none. A value
// it cannot read as a count is an error, not zero: guessing "complete" would
// let a caller treat a partial search as whole.
func (r *ServiceLogsResponse) chunksFailed() (int, error) {
	var failed float64
	switch v := r.SearchStats["chunks_failed"].(type) {
	case nil:
		return 0, nil
	case float64:
		failed = v
	case int:
		failed = float64(v)
	case int64:
		failed = float64(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("unreadable search_stats.chunks_failed %q", v)
		}
		failed = float64(n)
	default:
		return 0, fmt.Errorf("unreadable search_stats.chunks_failed of type %T", v)
	}
	if failed < 0 || failed != math.Trunc(failed) {
		return 0, fmt.Errorf("unreadable search_stats.chunks_failed %v", failed)
	}
	return int(failed), nil
}

// mayMissLines reports whether the returned lines may omit matching ones. An
// exact total settles it; a truncated search's total is only a floor, and
// without a total a full sample is assumed capped.
func (r *ServiceLogsResponse) mayMissLines(limit int) bool {
	switch {
	case r.LogsTruncated != nil && *r.LogsTruncated:
		return true
	case r.TotalMatchingLines != nil:
		return *r.TotalMatchingLines > len(r.Logs)
	default:
		return len(r.Logs) >= limit
	}
}
