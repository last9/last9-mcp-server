package utils

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// FakeNowEnv pins the query clock to a fixed instant. It exists only so evals
// and replays can resolve relative windows ("last 1h") reproducibly. Never
// set it in production.
const FakeNowEnv = "LAST9_FAKE_NOW_ISO"

var fakeNow *time.Time

// InitClock reads LAST9_FAKE_NOW_ISO once. Unset means real time; a set but
// invalid value is an error (never a silent fallback). It returns the pinned
// instant, or nil when the fake clock is inactive. Call once at startup.
func InitClock() (*time.Time, error) {
	raw := strings.TrimSpace(os.Getenv(FakeNowEnv))
	if raw == "" {
		fakeNow = nil
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q: want an RFC3339 timestamp, e.g. 2026-01-02T15:04:05Z: %w", FakeNowEnv, raw, err)
	}
	fakeNow = &t
	return &t, nil
}

// Now is the clock for query windows, lookbacks and defaults. Use it instead
// of time.Now() for those; keep time.Now() for timeouts, caches and token expiry.
func Now() time.Time {
	if fakeNow != nil {
		return *fakeNow
	}
	return time.Now()
}
