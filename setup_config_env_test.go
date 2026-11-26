package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"last9-mcp/internal/models"
)

const setupConfigHelpToken = "refresh-token-must-not-appear-in-help"

func TestSetupConfigDoesNotExposeRefreshTokenInHelp(t *testing.T) {
	if os.Getenv("TEST_SETUP_CONFIG_HELP") == "1" {
		os.Args = []string{"last9-mcp", "-help"}
		_, _ = SetupConfig(models.Config{})
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSetupConfigDoesNotExposeRefreshTokenInHelp$")
	cmd.Env = append(os.Environ(),
		"TEST_SETUP_CONFIG_HELP=1",
		"LAST9_REFRESH_TOKEN="+setupConfigHelpToken,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help subprocess failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), setupConfigHelpToken) {
		t.Fatalf("help output exposed LAST9_REFRESH_TOKEN:\n%s", output)
	}
}

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
