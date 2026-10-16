package guardrail

import (
	"testing"
	"time"

	"last9-mcp/internal/models"
)

func TestParseMode(t *testing.T) {
	valid := map[string]string{"": ModeOff, "off": ModeOff, " Shadow ": ModeShadow, "ENFORCE": ModeEnforce}
	for in, want := range valid {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"on", "block", "warn"} {
		if _, err := ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) should fail", in)
		}
	}
}

func TestValidate(t *testing.T) {
	valid := models.QueryGuardrail{Mode: "Enforce", RangeCap: time.Hour, ScopeThresholdBytes: 1, QueryBudgetBytes: 1}
	c := valid
	if err := Validate(&c); err != nil || c.Mode != ModeEnforce {
		t.Fatalf("Validate(valid) = %v, mode %q", err, c.Mode)
	}
	off := models.QueryGuardrail{}
	if err := Validate(&off); err != nil || off.Mode != ModeOff {
		t.Fatalf("Validate(zero) = %v, mode %q; zero thresholds are fine when off", err, off.Mode)
	}
	invalid := map[string]func(*models.QueryGuardrail){
		"mode":                 func(c *models.QueryGuardrail) { c.Mode = "block" },
		"zero range cap":       func(c *models.QueryGuardrail) { c.RangeCap = 0 },
		"zero scope threshold": func(c *models.QueryGuardrail) { c.ScopeThresholdBytes = 0 },
		"zero query budget":    func(c *models.QueryGuardrail) { c.QueryBudgetBytes = 0 },
		"negative session":     func(c *models.QueryGuardrail) { c.SessionBudgetBytes = -1 },
	}
	for name, mutate := range invalid {
		c := valid
		mutate(&c)
		if err := Validate(&c); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
