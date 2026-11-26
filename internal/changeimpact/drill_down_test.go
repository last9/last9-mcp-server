package changeimpact

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
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
	for _, d := range report.DrillDown {
		if d.Tool != "get_service_performance_details" {
			continue
		}
		raw := d.Arguments["include_series"] == true
		if mentionsSeries := strings.Contains(d.Why, "time series"); raw != mentionsSeries {
			t.Fatalf("include_series=%v but why = %q", d.Arguments["include_series"], d.Why)
		}
		if !raw && !strings.Contains(d.Why, "summary") {
			t.Fatalf("summary-mode drill-down must say it returns a summary: %q", d.Why)
		}
	}
}

// get_alerts rejects windows over an hour, so a longer after window needs one
// drill-down per slice for every alert Analyze reports to stay reachable.
func TestDrillDownAlertsCoverAnalyzeAfterWindowOverOneHour(t *testing.T) {
	change := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
	args := serviceArgs(change)
	args.AfterMinutes = 120
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow(),
		alerts: []alerting.AlertRuleData{{
			RuleID: "early", AlertGroupName: "checkout",
			Alerts: []alerting.AlertInstance{{Since: change.Add(10 * time.Minute).Unix()}},
		}},
	}
	report, err := runTool(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Alerts.Started) != 1 || report.Alerts.Started[0].RuleID != "early" {
		t.Fatalf("Analyze must keep the T+10m alert: %+v", report.Alerts.Started)
	}
	type slice struct {
		timeISO string
		window  any
	}
	var got []slice
	for _, d := range report.DrillDown {
		if d.Tool == "get_alerts" {
			got = append(got, slice{d.Arguments["time_iso"].(string), d.Arguments["window"]})
		}
	}
	afterEnd := change.Add(2 * time.Hour)
	want := []slice{
		{afterEnd.Format(time.RFC3339), float64(alerting.MaxAlertsMonitorWindowSeconds)},
		{afterEnd.Add(-time.Hour).Format(time.RFC3339), float64(alerting.MaxAlertsMonitorWindowSeconds)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("get_alerts drill_down = %+v, want hour slices covering the after window %+v", got, want)
	}
}

func TestDrillDownAlertsWindowUnderOneHourIsOneCall(t *testing.T) {
	change := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
	args := serviceArgs(change)
	args.AfterMinutes = 45
	report, err := runTool(t, &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}, args)
	if err != nil {
		t.Fatal(err)
	}
	var windows []any
	for _, d := range report.DrillDown {
		if d.Tool == "get_alerts" {
			windows = append(windows, d.Arguments["window"])
		}
	}
	if !reflect.DeepEqual(windows, []any{float64(45 * 60)}) {
		t.Fatalf("get_alerts windows = %v, want one 2700s call", windows)
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
			report, err := runToolWith(t, f, serviceArgs(change), func(cfg *models.Config) { cfg.ToolFilter.Tools = allowed })
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
