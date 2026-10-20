package changeimpact

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// apmUnavailablePath is where the report serialises APMDeviations.Unavailable.
const apmUnavailablePath = "deviations.apm.unavailable"

func TestAPMUnavailablePathMatchesReportJSON(t *testing.T) {
	var report Report
	report.Deviations.APM.Unavailable = []string{"error rate"}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	for _, key := range strings.Split(apmUnavailablePath, ".") {
		object, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("%q: %v is not an object", key, node)
		}
		if node, ok = object[key]; !ok {
			t.Fatalf("report JSON has no %q on the path %q", key, apmUnavailablePath)
		}
	}
}

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
		"`" + VerdictPossibleChange + "`",
		fmt.Sprintf("the %d windows right before T", bandWindows),
		fmt.Sprintf("With at least %d,", bandMinWindows),
		fmt.Sprintf("median + %g×MAD", bandMADMultiplier),
		fmt.Sprintf("or %g%% (p95) over the median", bandP95MinMovePct),
		"`baseline_band`",
		"valid answer",
		fmt.Sprintf("%g percentage point", errorRateIncreasePP),
		fmt.Sprintf("p95 up at least %g%%", p95LatencyIncreasePct),
		fmt.Sprintf("throughput (rpm) down at least %g%%", throughputDropPct),
		fmt.Sprintf("%d requests", minRequestsPerWindow),
		fmt.Sprintf("%d minutes", minAfterMinutes),
		fmt.Sprintf("default %d", defaultBaselineMinutes),
		fmt.Sprintf("%d..%d", minWindowMinutes, maxWindowMinutes),
		fmt.Sprintf("top %d", maxDependents),
		fmt.Sprintf("%d per list", sectionTopN),
		"get_change_events",
		"rounded down to the minute",
		"wholly or in any query listed in `" + apmUnavailablePath + "`",
		"Trace-attribute deviations explain a verdict but never set one",
		"`latency_env`",
		"also counts error-status spans and 429s",
		"client-only ones show 0 rpm",
		"label, else annotation",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("description must state %q", want)
		}
	}
	// Models look up the key literally: name it by its full JSON path every time.
	if n, full := strings.Count(description, "apm.unavailable`"), strings.Count(description, "`"+apmUnavailablePath+"`"); n != full {
		t.Errorf("description names apm.unavailable %d times, only %d by its full path %q", n, full, apmUnavailablePath)
	}
}
