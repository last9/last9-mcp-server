package logs

import (
	"testing"
	"time"

	"last9-mcp/internal/utils"
)

func TestLogsTimeRangeAnchoredAtFakeNow(t *testing.T) {
	t.Cleanup(func() { t.Setenv(utils.FakeNowEnv, ""); _, _ = utils.InitClock() })
	t.Setenv(utils.FakeNowEnv, "2026-01-02T03:04:05Z")
	if _, err := utils.InitClock(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	start, end, err := parseTimeRangeFromArgs(GetLogsArgs{LookbackMinutes: 15})
	if err != nil {
		t.Fatal(err)
	}
	if end != now.UnixMilli() || start != now.Add(-15*time.Minute).UnixMilli() {
		t.Fatalf("window %d..%d, want 15m ending %d", start, end, now.UnixMilli())
	}
}
