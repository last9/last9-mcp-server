package alerting

import (
	"fmt"
	"strings"
	"time"
)

// formatRuleLifecycle renders the pause, mute and recurring-schedule state that
// patch_alert writes, so get_alert_config and get_entity_alert_rules can read it back.
func formatRuleLifecycle(rule AlertRule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  Disabled: %t\n", ruleIsDisabled(rule))
	fmt.Fprintf(&b, "  Muted until: %s\n", formatMuteUntil(rule.MuteUntil))
	writeRecurringMuteSchedule(&b, rule.RecurringMuteSchedule)
	if rule.NextTransitionAt != nil && *rule.NextTransitionAt > 0 {
		fmt.Fprintf(&b, "  Next transition: %s\n", formatEpochSeconds(*rule.NextTransitionAt))
	}
	return b.String()
}

// The API only reports is_disabled on newer backends; older ones fold it into state.
func ruleIsDisabled(rule AlertRule) bool {
	if rule.IsDisabled != nil {
		return *rule.IsDisabled
	}
	return strings.EqualFold(strings.TrimSpace(rule.State), "disabled")
}

func formatMuteUntil(muteUntil int64) string {
	switch {
	case muteUntil == 0:
		return "none"
	case muteUntil < 0:
		return "indefinitely"
	default:
		return formatEpochSeconds(muteUntil)
	}
}

func formatEpochSeconds(sec int64) string {
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

func writeRecurringMuteSchedule(b *strings.Builder, s *RecurringMuteSchedule) {
	if s == nil {
		b.WriteString("  Recurring mute schedule: none\n")
		return
	}
	fmt.Fprintf(b, "  Recurring mute schedule: timezone %s", s.Timezone)
	if !s.Enabled {
		b.WriteString(" (enabled: false)")
	}
	b.WriteString("\n")
	for _, w := range s.Windows {
		fmt.Fprintf(b, "    %s %s-%s\n", formatWeekdays(w.Weekdays), w.StartTime, w.EndTime)
	}
}

func formatWeekdays(weekdays []string) string {
	days := make([]string, 0, len(weekdays))
	for _, d := range weekdays {
		if d = strings.TrimSpace(d); d != "" {
			days = append(days, strings.ToUpper(d[:1])+strings.ToLower(d[1:]))
		}
	}
	if len(days) == 0 {
		return "(no weekdays)"
	}
	return strings.Join(days, ",")
}
