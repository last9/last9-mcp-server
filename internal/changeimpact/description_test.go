package changeimpact

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The description promises these values to the model; pin them to the code.
func TestDescriptionDocumentsVerdictsAndThresholds(t *testing.T) {
	raw, err := os.ReadFile("../prompts/descriptions/analyze_change_impact.md")
	if err != nil {
		t.Fatal(err)
	}
	description := string(raw)
	for _, want := range []string{
		"`" + VerdictRegressionLikely + "`",
		"`" + VerdictNoSignificantChange + "`",
		"`" + VerdictInsufficientData + "`",
		"valid answer",
		fmt.Sprintf("%g percentage point", errorRateIncreasePP),
		fmt.Sprintf("%g%%", p95LatencyIncreasePct),
		fmt.Sprintf("%d requests", minRequestsPerWindow),
		fmt.Sprintf("%d minutes", minAfterMinutes),
		fmt.Sprintf("default %d", defaultBaselineMinutes),
		fmt.Sprintf("%d..%d", minWindowMinutes, maxWindowMinutes),
		fmt.Sprintf("top %d", maxDependents),
		fmt.Sprintf("%d per list", sectionTopN),
		"get_change_events",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("description must state %q", want)
		}
	}
}
