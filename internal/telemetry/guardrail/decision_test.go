package guardrail

import (
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/models"
)

var testLimits = models.QueryGuardrail{
	Mode:                ModeEnforce,
	RangeCap:            24 * time.Hour,
	ScopeThresholdBytes: 1000,
	QueryBudgetBytes:    5000,
	SessionBudgetBytes:  100_000,
}

func bytesPtr(n int64) *int64 { return &n }

func TestDecide(t *testing.T) {
	cap := testLimits.RangeCap
	tests := []struct {
		name   string
		scan   scan
		action Action
		reason string
	}{
		{"unscoped range cap-1s", scan{Range: cap - time.Second}, Allow, ""},
		{"unscoped range at cap", scan{Range: cap}, Allow, ""},
		{"unscoped range cap+1s", scan{Range: cap + time.Second}, Reject, ReasonRangeTooWide},
		{"scoped range cap+1s", scan{Range: cap + time.Second, Scoped: true}, Allow, ""},

		{"unscoped estimate threshold-1", scan{Range: time.Hour, Estimate: bytesPtr(999)}, Allow, ""},
		{"unscoped estimate at threshold", scan{Range: time.Hour, Estimate: bytesPtr(1000)}, Allow, ""},
		{"unscoped estimate threshold+1", scan{Range: time.Hour, Estimate: bytesPtr(1001)}, Reject, ReasonUnscopedQuery},
		{"scoped estimate threshold+1", scan{Range: time.Hour, Scoped: true, Estimate: bytesPtr(1001)}, Allow, ""},
		{"server-scoped estimate threshold+1", scan{Range: time.Hour, ServerScoped: true, Estimate: bytesPtr(1001)}, Allow, ""},
		{"server-scoped range cap+1s", scan{Range: cap + time.Second, ServerScoped: true, Estimate: bytesPtr(1)}, Reject, ReasonRangeTooWide},

		{"scoped estimate budget-1", scan{Range: time.Hour, Scoped: true, Estimate: bytesPtr(4999)}, Allow, ""},
		{"scoped estimate at budget", scan{Range: time.Hour, Scoped: true, Estimate: bytesPtr(5000)}, Allow, ""},
		{"scoped estimate budget+1", scan{Range: time.Hour, Scoped: true, Estimate: bytesPtr(5001)}, Reject, ReasonScanBudgetExceeded},

		{"range check runs before scope", scan{Range: cap + time.Second, Estimate: bytesPtr(5001)}, Reject, ReasonRangeTooWide},
		{"scope check runs before budget", scan{Range: time.Hour, Estimate: bytesPtr(5001)}, Reject, ReasonUnscopedQuery},
		{"no estimate skips scope and budget", scan{Range: cap}, Allow, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decide(testLimits, tt.scan)
			if got.Action != tt.action || got.Reason != tt.reason {
				t.Fatalf("decide = %s/%q, want %s/%q", got.Action, got.Reason, tt.action, tt.reason)
			}
			if tt.action == Reject && got.Guidance == "" {
				t.Error("a reject must carry guidance")
			}
		})
	}
}

func TestDecide_ScopeThresholdAboveBudgetLeavesBudgetToReject(t *testing.T) {
	limits := testLimits
	limits.ScopeThresholdBytes = 10_000
	got := decide(limits, scan{Range: time.Hour, Estimate: bytesPtr(5001)})
	if got.Reason != ReasonScanBudgetExceeded {
		t.Fatalf("reason = %q, want %q", got.Reason, ReasonScanBudgetExceeded)
	}
}

func TestGuidance(t *testing.T) {
	t.Run("range names the cap and both remedies", func(t *testing.T) {
		g := decide(testLimits, scan{Range: 72 * time.Hour}).Guidance
		for _, want := range []string{"72h", "24h", "ServiceName"} {
			if !strings.Contains(g, want) {
				t.Errorf("guidance %q lacks %q", g, want)
			}
		}
	})
	t.Run("unscoped names the estimate, the threshold and a service filter", func(t *testing.T) {
		limits := testLimits
		limits.ScopeThresholdBytes = 10 << 30
		limits.QueryBudgetBytes = 1 << 40
		g := decide(limits, scan{Range: time.Hour, Estimate: bytesPtr(15 << 30)}).Guidance
		for _, want := range []string{"15.0 GiB", "10.0 GiB", `"ServiceName"`} {
			if !strings.Contains(g, want) {
				t.Errorf("guidance %q lacks %q", g, want)
			}
		}
	})
	t.Run("budget on a scoped query suggests a narrower window and aggregating", func(t *testing.T) {
		limits := testLimits
		limits.QueryBudgetBytes = 100 << 30
		g := decide(limits, scan{Range: 24 * time.Hour, Scoped: true, Estimate: bytesPtr(400 << 30)}).Guidance
		for _, want := range []string{"400.0 GiB", "100.0 GiB", "last 6h", "aggregate first"} {
			if !strings.Contains(g, want) {
				t.Errorf("guidance %q lacks %q", g, want)
			}
		}
		if strings.Contains(g, `"ServiceName"`) {
			t.Errorf("guidance %q suggests a service filter on an already scoped query", g)
		}
	})
	t.Run("budget on an unscoped query also suggests a service filter", func(t *testing.T) {
		limits := testLimits
		limits.ScopeThresholdBytes = 1 << 40
		limits.QueryBudgetBytes = 100 << 30
		g := decide(limits, scan{Range: time.Hour, Estimate: bytesPtr(400 << 30)}).Guidance
		for _, want := range []string{`"ServiceName"`, "last 15m"} {
			if !strings.Contains(g, want) {
				t.Errorf("guidance %q lacks %q", g, want)
			}
		}
	})
	t.Run("budget omits a window under a minute", func(t *testing.T) {
		limits := testLimits
		g := decide(limits, scan{Range: time.Minute, Scoped: true, Estimate: bytesPtr(1 << 30)}).Guidance
		if strings.Contains(g, "last ") {
			t.Errorf("guidance %q suggests a sub-minute window", g)
		}
	})
}

func TestSessionDecision(t *testing.T) {
	budget := testLimits.SessionBudgetBytes
	tests := []struct {
		name   string
		used   int64
		budget int64
		action Action
	}{
		{"79%", budget * 79 / 100, budget, Allow},
		{"80%", budget * 80 / 100, budget, Warn},
		{"100%", budget, budget, Warn},
		{"over budget still only warns", budget * 2, budget, Warn},
		{"disabled", budget * 2, 0, Allow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionDecision(tt.budget, tt.used)
			if got.Action != tt.action {
				t.Fatalf("action = %s, want %s", got.Action, tt.action)
			}
			if tt.action == Warn && (got.Reason != ReasonSessionBudget80 || !strings.Contains(got.Guidance, "%")) {
				t.Errorf("decision = %+v", got)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 10 << 30: "10.0 GiB", 3 << 40: "3.0 TiB"}
	for in, want := range tests {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
