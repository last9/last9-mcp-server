package apm

import (
	"context"
	"net/http"
	"testing"
	"time"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const lookbackFloorNoCompletedBuckets = "requested window contains no completed buckets"

// TestDeviationLookbackFloorIntegerOneErrors pins the documented production
// behavior: the schema-advertised integer minimum (lookback_minutes: 1)
// collapses to a zero-length effective window under a non-step-aligned clock
// and returns the "requested window contains no completed buckets" domain
// error. The resolver's strict-containment invariant is intentional; this
// guards the documented behavior against a silent change so the contract
// surfaces never drift from what the handler enforces.
func TestDeviationLookbackFloorIntegerOneErrors(t *testing.T) {
	deps := testDeviationHandlerDeps() // now = 2026-07-11T10:07:32Z, queryStep = 1m (non-minute-aligned)
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeviationArgs{LookbackMinutes: 1})
	if err == nil {
		t.Fatal("expected 'no completed buckets' error for integer lookback_minutes=1 at a non-step-aligned now, got nil")
	}
	if err.Error() != lookbackFloorNoCompletedBuckets {
		t.Fatalf("unexpected error for lookback_minutes=1: %v", err)
	}
}

// TestDeviationLookbackFloorResolverBoundary pins the resolver's now-dependent
// continuous success boundary at the fixture now (1 + subStepOffset/queryStep,
// ~1.5333 here). Collapsing values below the boundary error; fractional values
// above it succeed. This guards against a future "fix" that raises the schema
// minimum to 2, which would over-reject valid fractional inputs like 1.6/1.9
// that the type:number schema legitimately admits at some now offsets.
func TestDeviationLookbackFloorResolverBoundary(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC) // subStepOffset = 32s, queryStep = 1m
	tests := []struct {
		lookback float64
		wantErr  bool
	}{
		{1.0, true},  // integer minimum the schema advertises: always collapses in production
		{1.5, true},  // below the ~1.5333 threshold: collapses
		{1.6, false}, // above threshold: one completed bucket
		{1.9, false},
		{2.0, false}, // worst-case all-now integer floor: one completed bucket
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			_, err := resolveDeviationWindows(DeviationArgs{LookbackMinutes: tt.lookback}, now, time.Minute)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Fatalf("lookback=%v: got err=%v want err=%t", tt.lookback, err, tt.wantErr)
			}
			if gotErr && err.Error() != lookbackFloorNoCompletedBuckets {
				t.Fatalf("lookback=%v: unexpected error %v", tt.lookback, err)
			}
		})
	}
}
