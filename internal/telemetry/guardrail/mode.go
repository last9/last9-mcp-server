package guardrail

import (
	"fmt"
	"strings"
	"time"

	"last9-mcp/internal/models"
)

const (
	ModeOff     = "off"
	ModeShadow  = "shadow"
	ModeEnforce = "enforce"
)

// Off by default until the estimate routes are live: before then shadow pays
// an estimate round trip per query to learn nothing. The thresholds are
// starting points for shadow mode; tune them before switching to enforce.
const (
	DefaultMode                = ModeOff
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

// Validate normalises the mode and rejects thresholds no query could pass.
func Validate(c *models.QueryGuardrail) error {
	mode, err := ParseMode(c.Mode)
	if err != nil {
		return err
	}
	c.Mode = mode
	if mode == ModeOff {
		return nil
	}
	switch {
	case c.RangeCap <= 0:
		return fmt.Errorf("query guardrail range cap must be positive, got %s", c.RangeCap)
	case c.ScopeThresholdBytes <= 0:
		return fmt.Errorf("query guardrail scope threshold must be positive, got %d", c.ScopeThresholdBytes)
	case c.QueryBudgetBytes <= 0:
		return fmt.Errorf("query guardrail query budget must be positive, got %d", c.QueryBudgetBytes)
	case c.SessionBudgetBytes < 0:
		return fmt.Errorf("query guardrail session budget must be 0 (off) or positive, got %d", c.SessionBudgetBytes)
	}
	return nil
}
