package guardrail

import (
	"fmt"
	"strings"
	"time"
)

const (
	ModeOff     = "off"
	ModeShadow  = "shadow"
	ModeEnforce = "enforce"
)

// Defaults are starting points for shadow mode; tune them from its telemetry
// before switching to enforce.
const (
	DefaultMode                = ModeShadow
	DefaultRangeCap            = 24 * time.Hour
	DefaultScopeThresholdBytes = 50 << 30
	DefaultQueryBudgetBytes    = 500 << 30
	DefaultSessionBudgetBytes  = 5 << 40
)

// ParseMode normalises a configured mode; empty means off.
func ParseMode(s string) (string, error) {
	switch mode := strings.ToLower(strings.TrimSpace(s)); mode {
	case "", ModeOff:
		return ModeOff, nil
	case ModeShadow, ModeEnforce:
		return mode, nil
	}
	return "", fmt.Errorf("invalid query guardrail mode %q: want off, shadow or enforce", s)
}
