package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const maxWindowDays = math.MaxInt64 / 24

// NormalizeWindowAggregateDays rewrites window_aggregate windows given in days
// as the same width in hours, because the query API rejects the day unit.
func NormalizeWindowAggregateDays(pipeline []map[string]interface{}) error {
	for i, stage := range pipeline {
		if stageType, _ := stage["type"].(string); stageType != "window_aggregate" {
			continue
		}
		window, ok := stage["window"].([]interface{})
		if !ok || len(window) != 2 {
			continue
		}
		unit, _ := window[1].(string)
		if !strings.EqualFold(unit, "days") && !strings.EqualFold(unit, "day") {
			continue
		}
		days, ok := wholeDays(window[0])
		if !ok {
			return fmt.Errorf("window_aggregate at stage %d: window %v must be a whole number of days from 1 to %d; use hours or minutes for shorter buckets", i, window, int64(maxWindowDays))
		}
		stage["window"] = []interface{}{strconv.FormatInt(days*24, 10), "hours"}
	}
	return nil
}

func wholeDays(raw interface{}) (int64, bool) {
	var days int64
	switch v := raw.(type) {
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		days = n
	case float64:
		if v != math.Trunc(v) || v < 1 || v > maxWindowDays {
			return 0, false
		}
		days = int64(v)
	default:
		return 0, false
	}
	return days, days >= 1 && days <= maxWindowDays
}
