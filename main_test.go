package main

import (
	"os"
	"testing"

	"last9-mcp/internal/models"
)

func TestSetupConfigDefaultsMaxGetLogsEntries(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() {
		os.Args = originalArgs
	})

	os.Args = []string{"last9-mcp"}

	cfg, err := SetupConfig(models.Config{
		RefreshToken: "test-refresh-token",
	})
	if err != nil {
		t.Fatalf("SetupConfig returned error: %v", err)
	}

	if cfg.MaxGetLogsEntries != models.DefaultMaxGetLogsEntries {
		t.Fatalf("MaxGetLogsEntries = %d, want %d", cfg.MaxGetLogsEntries, models.DefaultMaxGetLogsEntries)
	}
}

func TestSetupConfigReadsMaxGetLogsEntriesFromEnv(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() {
		os.Args = originalArgs
	})

	t.Setenv("LAST9_MAX_GET_LOGS_ENTRIES", "1234")
	os.Args = []string{"last9-mcp"}

	cfg, err := SetupConfig(models.Config{
		RefreshToken: "test-refresh-token",
	})
	if err != nil {
		t.Fatalf("SetupConfig returned error: %v", err)
	}

	if cfg.MaxGetLogsEntries != 1234 {
		t.Fatalf("MaxGetLogsEntries = %d, want 1234", cfg.MaxGetLogsEntries)
	}
}

func TestSetupConfigFallsBackToDefaultMaxGetLogsEntries(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() {
		os.Args = originalArgs
	})

	os.Args = []string{"last9-mcp", "-max_get_logs_entries=0"}

	cfg, err := SetupConfig(models.Config{
		RefreshToken: "test-refresh-token",
	})
	if err != nil {
		t.Fatalf("SetupConfig returned error: %v", err)
	}

	if cfg.MaxGetLogsEntries != models.DefaultMaxGetLogsEntries {
		t.Fatalf("MaxGetLogsEntries = %d, want %d", cfg.MaxGetLogsEntries, models.DefaultMaxGetLogsEntries)
	}
}
