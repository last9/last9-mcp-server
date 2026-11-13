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
