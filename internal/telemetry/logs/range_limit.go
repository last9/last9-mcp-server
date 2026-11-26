package logs

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	rangeLimitRejectionPattern = regexp.MustCompile(`(?i)\b(?:time\s+range|range|duration|window)\b.*\bexceed(?:s|ed)?\b.*\b(?:limit|max|maximum)\b`)
	rangeLimitValuePattern     = regexp.MustCompile(`(?i)\b(?:limit|max|maximum)\b\D{0,30}?(\d{1,6})\s*(hours?|days?|h|d)(?:[^a-z]|$)`)
)

// Aggregates run as one request, so only a shorter window resolves a range-limit
// 400; narrowing is left to the caller because it answers for a different period.
func rangeLimitResult(err error, pipeline []map[string]interface{}, window utils.TimeChunk) *mcp.CallToolResult {
	var upstream *utils.UpstreamHTTPError
	if !utils.PipelineHasAggregateStage(pipeline) || !errors.As(err, &upstream) ||
		upstream.StatusCode != http.StatusBadRequest || !rangeLimitRejectionPattern.MatchString(upstream.Body) {
		return nil
	}

	span := time.Duration(window.EndMs-window.StartMs) * time.Millisecond
	requested := fmt.Sprintf("The requested %s range (%s to %s)", formatRangeDuration(span),
		time.UnixMilli(window.StartMs).UTC().Format(time.RFC3339), time.UnixMilli(window.EndMs).UTC().Format(time.RFC3339))
	limit, limitOK := reportedRangeLimit(upstream.Body)
	// A limit the range already fits cannot explain the rejection, so no shorter
	// range derived from it would be accepted.
	if limitOK && span <= limit {
		return utils.ToolErrorResult(fmt.Sprintf("get_logs aggregate query rejected: %s was rejected as too long, but it is within the reported %s limit, so that limit does not say how much shorter to go. Do not retry unchanged or guess a shorter range: report the rejection to the user.",
			requested, formatRangeDuration(limit)))
	}
	var msg string
	if limitOK {
		msg = fmt.Sprintf("%s exceeds the reported %s limit. Choose lookback_minutes of %d (%s) or less, or move start_time_iso and end_time_iso to span at most %s.",
			requested, formatRangeDuration(limit), int64(limit/time.Minute), formatRangeSpan(limit), formatRangeSpan(limit))
	} else {
		msg = requested + " exceeds the maximum range the log backend accepts; the response gave no limit. Shorten lookback_minutes, or move start_time_iso and end_time_iso closer together."
	}
	return utils.ToolErrorResult("get_logs aggregate query rejected: " + msg +
		" A shorter range gives a total for that shorter period, not the requested one: if the original period matters, confirm with the user before narrowing. Do not retry unchanged.")
}

func reportedRangeLimit(body string) (time.Duration, bool) {
	match := rangeLimitValuePattern.FindStringSubmatch(body)
	if match == nil {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	if err != nil || value <= 0 {
		return 0, false
	}
	if strings.HasPrefix(strings.ToLower(match[2]), "d") {
		return time.Duration(value) * 24 * time.Hour, true
	}
	return time.Duration(value) * time.Hour, true
}

func formatRangeDuration(d time.Duration) string {
	value, unit := rangeDurationParts(d)
	return fmt.Sprintf("%d-%s", value, unit)
}

func formatRangeSpan(d time.Duration) string {
	value, unit := rangeDurationParts(d)
	if value == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", value, unit)
}

func rangeDurationParts(d time.Duration) (int64, string) {
	switch {
	case d%(24*time.Hour) == 0:
		return int64(d / (24 * time.Hour)), "day"
	case d%time.Hour == 0:
		return int64(d / time.Hour), "hour"
	default:
		return int64((d + time.Minute - 1) / time.Minute), "minute"
	}
}
