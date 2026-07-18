package toolsets

import (
	"strings"
	"testing"
)

func TestParseEmptyIsAll(t *testing.T) {
	for _, spec := range []string{"", "  ", ","} {
		set, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if set != nil {
			t.Fatalf("Parse(%q): want nil (all tools), got %v", spec, set)
		}
	}
}

func TestParseAllSupersedes(t *testing.T) {
	set, err := Parse("all,logs")
	if err != nil {
		t.Fatal(err)
	}
	if set != nil {
		t.Fatalf("all should supersede; got %v", set)
	}
}

func TestParseInvestigate(t *testing.T) {
	set, err := Parse("investigate")
	if err != nil {
		t.Fatal(err)
	}
	if set == nil {
		t.Fatal("investigate must not expand to nil/all")
	}
	for _, want := range []string{"get_logs", "get_traces", "prometheus_instant_query", "did_you_mean", "list_datasources", "get_apm_service_deviations"} {
		if !set.Allows(want) {
			t.Errorf("investigate missing %q", want)
		}
	}
	for _, deny := range []string{"get_alerts", "list_dashboards", "create_dashboard", "add_drop_rule", "list_dashboard_snapshots"} {
		if set.Allows(deny) {
			t.Errorf("investigate should exclude %q", deny)
		}
	}
}

func TestParseUnion(t *testing.T) {
	set, err := Parse("logs,alerts")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Allows("get_logs") || !set.Allows("get_alerts") {
		t.Fatal("union missing expected tools")
	}
	if set.Allows("get_traces") {
		t.Fatal("union should not include traces")
	}
}

func TestParseUnknown(t *testing.T) {
	_, err := Parse("nope")
	if err == nil {
		t.Fatal("expected error for unknown toolset")
	}
	msg := err.Error()
	for _, name := range []string{"logs", "investigate", "all"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error should list %q; got %q", name, msg)
		}
	}
}

func TestNilSetAllowsEverything(t *testing.T) {
	var set Set
	if !set.Allows("anything") {
		t.Fatal("nil Set must allow all tools")
	}
}
