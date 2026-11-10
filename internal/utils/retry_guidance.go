package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ResultGuidanceOptions struct {
	Window           string
	ServiceName      string
	Environment      string
	DBSystem         string
	AdditionalChecks []string
}

type TruncatedResultOptions struct {
	Subject  string
	Returned int
	Total    int
	MaxLimit int
}

func EmptyResultAdvice(subject string, opts ResultGuidanceOptions) string {
	actions := emptyResultActions(opts)
	return fmt.Sprintf("%s. Try: %s", emptyResultSentence(subject, opts), NumberedActions(actions))
}

func EmptyResultGuidanceMap(subject string, opts ResultGuidanceOptions) map[string]any {
	message := emptyResultSentence(subject, opts)
	actions := emptyResultActions(opts)
	return map[string]any{
		"message":    message,
		"next_steps": actions,
	}
}

func TruncatedResultAdvice(opts TruncatedResultOptions) string {
	var message string
	if opts.Total > 0 {
		message = fmt.Sprintf("Returned %s of %s %s.", FormatCount(opts.Returned), FormatCount(opts.Total), opts.Subject)
	} else {
		message = fmt.Sprintf("Returned the first %s %s; more results may match.", FormatCount(opts.Returned), opts.Subject)
	}
	actions := []string{"use `start_time_iso`/`end_time_iso` to narrow range"}
	if opts.Total > opts.Returned && opts.MaxLimit == 0 {
		actions = append(actions, "increase limit to return more rows")
	} else if opts.MaxLimit > 0 && opts.Returned < opts.MaxLimit {
		actions = append(actions, fmt.Sprintf("increase limit up to %s", FormatCount(opts.MaxLimit)))
	} else {
		actions = append(actions, "add more selective filters such as service or environment")
	}
	return message + " Try: " + NumberedActions(actions)
}

func NumberedActions(actions []string) string {
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("(%d) %s", len(parts)+1, action))
	}
	return strings.Join(parts, ", ")
}

func WindowFromMinutes(minutes int) string {
	if minutes <= 0 {
		return "the selected time range"
	}
	if minutes%60 == 0 {
		hours := minutes / 60
		return fmt.Sprintf("last %dh", hours)
	}
	return fmt.Sprintf("last %dm", minutes)
}

func WindowFromRange(start, end time.Time) string {
	minutes := int(end.Sub(start).Minutes())
	if end.Sub(start)%time.Minute != 0 {
		minutes++
	}
	return WindowFromMinutes(minutes)
}

func FormatCount(n int) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return sign + s
	}
	var b strings.Builder
	b.WriteString(sign)
	prefix := len(s) % 3
	if prefix == 0 {
		prefix = 3
	}
	b.WriteString(s[:prefix])
	for i := prefix; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func FindStringFilter(query any, fieldNames ...string) string {
	fields := map[string]struct{}{}
	for _, field := range fieldNames {
		fields[field] = struct{}{}
	}
	return findStringFilter(query, fields)
}

func emptyResultSentence(subject string, opts ResultGuidanceOptions) string {
	parts := []string{"No " + subject + " found"}
	if opts.ServiceName != "" {
		parts = append(parts, "for service "+opts.ServiceName)
	}
	if opts.DBSystem != "" {
		parts = append(parts, "for db_system="+opts.DBSystem)
	}
	if opts.Environment != "" {
		parts = append(parts, "in environment "+opts.Environment)
	}
	if opts.Window != "" {
		parts = append(parts, "in "+opts.Window)
	}
	return strings.Join(parts, " ")
}

func emptyResultActions(opts ResultGuidanceOptions) []string {
	actions := []string{"widen the time range beyond the current window, for example to 6h when querying a shorter window"}
	if opts.ServiceName != "" {
		actions = append(actions, fmt.Sprintf("call `did_you_mean` with query='%s' to find similar service names", opts.ServiceName))
	} else {
		actions = append(actions, "call `did_you_mean` for any service or entity name you plan to query next")
	}
	if opts.Environment != "" {
		actions = append(actions, "verify the environment param")
	}
	return append(actions, opts.AdditionalChecks...)
}

func findStringFilter(value any, fields map[string]struct{}) string {
	switch value := value.(type) {
	case []map[string]interface{}:
		for _, item := range value {
			if found := findStringFilter(item, fields); found != "" {
				return found
			}
		}
	case []interface{}:
		if len(value) >= 2 {
			field, fieldOK := value[0].(string)
			filterValue, valueOK := value[1].(string)
			if fieldOK && valueOK {
				if _, ok := fields[field]; ok {
					return filterValue
				}
			}
		}
		for _, item := range value {
			if found := findStringFilter(item, fields); found != "" {
				return found
			}
		}
	case map[string]any:
		for _, item := range value {
			if found := findStringFilter(item, fields); found != "" {
				return found
			}
		}
	}
	return ""
}
