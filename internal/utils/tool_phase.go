package utils

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func StartToolPhase(ctx context.Context, tool, phase string) func(error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() && !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return func(error) {}
	}
	// Elapsed work uses the monotonic clock, independent of replay query time.
	started := time.Now()
	return func(err error) {
		duration := float64(time.Since(started)) / float64(time.Millisecond)
		span.SetAttributes(
			attribute.Float64("last9.phase."+phase+".duration_ms", duration),
			attribute.Bool("last9.phase."+phase+".failed", err != nil),
		)
		slog.DebugContext(ctx, "tool phase completed", "tool", tool, "phase", phase,
			"duration_ms", duration, "failed", err != nil)
	}
}
