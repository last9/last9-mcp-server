package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestToolPhaseDebugLog(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	finish := StartToolPhase(context.Background(), "get_log_patterns", "sample")
	finish(errors.New("sensitive upstream body"))
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["tool"] != "get_log_patterns" || entry["phase"] != "sample" || entry["failed"] != true {
		t.Fatalf("unexpected phase log: %v", entry)
	}
	if duration, ok := entry["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatalf("invalid duration: %v", entry)
	}
	if strings.Contains(output.String(), "sensitive") {
		t.Fatal("phase log exposed error text")
	}
}

func TestToolPhaseWithoutTracingOrDebugDoesNotLog(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	StartToolPhase(context.Background(), "get_log_patterns", "sample")(nil)
	if output.Len() != 0 {
		t.Fatalf("unexpected diagnostic: %s", output.String())
	}
}

func TestToolPhaseUsesElapsedTimeWithFrozenQueryClock(t *testing.T) {
	t.Setenv(FakeNowEnv, "2020-01-02T03:04:05Z")
	if _, err := InitClock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { t.Setenv(FakeNowEnv, ""); _, _ = InitClock() })
	ctx, finish := RecordedSpanContext(t)
	endPhase := StartToolPhase(ctx, "get_log_patterns", "sample")
	time.Sleep(5 * time.Millisecond)
	endPhase(nil)
	duration := finish()["last9.phase.sample.duration_ms"].AsFloat64()
	if duration < 5 || duration > float64(time.Hour/time.Millisecond) {
		t.Fatalf("elapsed milliseconds = %v, want real elapsed time independent of query clock", duration)
	}
}
