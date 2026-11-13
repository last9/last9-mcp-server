package main

import (
	"os"
	"testing"

	"last9-mcp/internal/models"
)

func TestSetupConfigReadsRefreshTokenFromEnv(t *testing.T) {
	t.Setenv("LAST9_REFRESH_TOKEN", "from-env")
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"last9-mcp"}

	cfg, err := SetupConfig(models.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RefreshToken != "from-env" {
		t.Fatalf("RefreshToken = %q, want value from LAST9_REFRESH_TOKEN", cfg.RefreshToken)
	}
}
