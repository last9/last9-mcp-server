package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/guardrail"
)

func setupConfigWithArgs(t *testing.T, args ...string) (models.Config, error) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = append([]string{"last9-mcp"}, args...)
	return SetupConfig(models.Config{RefreshToken: "test-token"})
}

func TestSetupConfig_QueryGuardrailDefaults(t *testing.T) {
	t.Setenv("LAST9_QUERY_GUARDRAIL", "")
	cfg, err := setupConfigWithArgs(t)
	if err != nil {
		t.Fatalf("SetupConfig: %v", err)
	}
	want := models.QueryGuardrail{
		Mode:                guardrail.ModeShadow,
		RangeCap:            24 * time.Hour,
		ScopeThresholdBytes: guardrail.DefaultScopeThresholdBytes,
		QueryBudgetBytes:    guardrail.DefaultQueryBudgetBytes,
		SessionBudgetBytes:  guardrail.DefaultSessionBudgetBytes,
	}
	if cfg.QueryGuardrail != want {
		t.Fatalf("QueryGuardrail = %+v, want %+v", cfg.QueryGuardrail, want)
	}
}

func TestSetupConfig_QueryGuardrailFromEnvAndFlags(t *testing.T) {
	t.Setenv("LAST9_QUERY_GUARDRAIL", "enforce")
	t.Setenv("LAST9_QUERY_GUARDRAIL_SESSION_BUDGET_BYTES", "0")
	cfg, err := setupConfigWithArgs(t, "--query_guardrail_range_cap=6h", "--query_guardrail_query_budget_bytes=1024")
	if err != nil {
		t.Fatalf("SetupConfig: %v", err)
	}
	g := cfg.QueryGuardrail
	if g.Mode != guardrail.ModeEnforce || g.RangeCap != 6*time.Hour || g.QueryBudgetBytes != 1024 || g.SessionBudgetBytes != 0 {
		t.Fatalf("QueryGuardrail = %+v", g)
	}
}

func TestSetupConfig_QueryGuardrailRejectsInvalidMode(t *testing.T) {
	t.Setenv("LAST9_QUERY_GUARDRAIL", "block")
	_, err := setupConfigWithArgs(t)
	if err == nil || !strings.Contains(err.Error(), "query guardrail mode") {
		t.Fatalf("err = %v, want an invalid mode error", err)
	}
}
