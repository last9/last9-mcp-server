package changeimpact

import (
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
)

func TestDrillDownNamesNextToolsWithWindows(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]map[string]any{}
	for _, d := range report.DrillDown {
		tools[d.Tool] = d.Arguments
	}
	for _, want := range []string{"get_service_performance_details", "get_apm_service_deviations", "get_service_traces", "get_alerts"} {
		if tools[want] == nil {
			t.Fatalf("drill_down missing %s: %+v", want, report.DrillDown)
		}
	}
	if tools["get_apm_service_deviations"]["baseline_end_time_iso"] != change.Format(time.RFC3339) || tools["get_service_traces"]["service_name"] != "checkout" {
		t.Fatalf("drill_down arguments = %+v", tools)
	}
}

func TestDrillDownNamesOnlyServedTools(t *testing.T) {
	for _, spec := range []string{"investigate", "alerts", "metrics,alerts,traces"} {
		t.Run(spec, func(t *testing.T) {
			allowed, err := toolsets.Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			change := recentChange()
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
			report, err := runToolWith(t, f, serviceArgs(change), func(cfg *models.Config) { cfg.AllowedTools = allowed })
			if err != nil {
				t.Fatal(err)
			}
			if len(report.DrillDown) == 0 && spec != "alerts" {
				t.Fatal("drill_down is empty")
			}
			for _, d := range report.DrillDown {
				if !allowed.Allows(d.Tool) {
					t.Fatalf("drill_down names %s, which --toolsets=%s does not serve", d.Tool, spec)
				}
			}
			if spec == "metrics,alerts,traces" && len(report.DrillDown) != 5 {
				t.Fatalf("drill_down = %+v, want all five tools when all are served", report.DrillDown)
			}
		})
	}
}
