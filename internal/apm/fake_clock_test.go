package apm

import (
	"testing"
	"time"

	"last9-mcp/internal/utils"
)

func TestInstantQueryTimeAnchoredAtFakeNow(t *testing.T) {
	t.Cleanup(func() { t.Setenv(utils.FakeNowEnv, ""); _, _ = utils.InitClock() })
	t.Setenv(utils.FakeNowEnv, "2026-01-02T03:04:05Z")
	if _, err := utils.InitClock(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	got, err := resolveInstantQueryTime("", 0)
	if err != nil || got != now.Unix() {
		t.Fatalf("no lookback: got %d, %v; want %d", got, err, now.Unix())
	}
	got, err = resolveInstantQueryTime("", 30)
	if err != nil || got != now.Add(-30*time.Minute).Unix() {
		t.Fatalf("30m lookback: got %d, %v; want %d", got, err, now.Add(-30*time.Minute).Unix())
	}
}
