package toolsets

import (
	"sort"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	investigate := func() map[string]bool {
		want := map[string]bool{}
		for _, domain := range []string{"logs", "traces", "metrics", "profiles"} {
			for _, tool := range named[domain] {
				want[tool] = true
			}
		}
		for _, tool := range investigateExtras {
			want[tool] = true
		}
		return want
	}()
	tests := []struct {
		name    string
		spec    string
		wantNil bool
		wantSet map[string]bool
		wantErr bool
	}{
		{name: "empty means all", spec: "", wantNil: true},
		{name: "whitespace trims and lowercases", spec: " Alerts ", wantSet: toSet(named["alerts"])},
		{name: "unknown token errors", spec: "bogus", wantErr: true},
		{name: "all supersedes others", spec: "all", wantNil: true},
		{name: "all supersedes later tokens", spec: "all,logs", wantNil: true},
		{name: "investigate composite excludes alerts domain", spec: "investigate", wantSet: investigate},
		{name: "uppercase valid token", spec: "METRICS", wantSet: toSet(named["metrics"])},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := Parse(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got set %v", set)
				}
				for _, want := range ValidNames() {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not list valid name %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantNil {
				if set != nil {
					t.Fatalf("want nil set, got %v", set)
				}
				return
			}
			if set == nil {
				t.Fatal("want populated set, got nil")
			}
			got := make(map[string]bool, len(set))
			for name := range set {
				got[name] = true
			}
			if len(got) != len(tt.wantSet) {
				t.Fatalf("set size = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.wantSet), sortedKeys(got), sortedKeys(tt.wantSet))
			}
			for name := range tt.wantSet {
				if !got[name] {
					t.Errorf("missing tool %q\ngot:  %v\nwant: %v", name, sortedKeys(got), sortedKeys(tt.wantSet))
				}
			}
		})
	}
}

func TestInvestigateExcludesAlertsDomainTools(t *testing.T) {
	set, err := Parse("investigate")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"get_alerts", "describe_alert_chart", "create_alert_from_chart", "get_drop_rules"} {
		if set.Allows(tool) {
			t.Errorf("investigate must not include alerts-domain tool %q", tool)
		}
	}
	if !set.Allows("did_you_mean") || !set.Allows("list_datasources") {
		t.Error("investigate must include its extras")
	}
}

func TestAlertsToolsetMembership(t *testing.T) {
	set, err := Parse("alerts")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"get_alerts", "add_drop_rule", "describe_alert_chart", "create_alert_from_chart"} {
		if !set.Allows(tool) {
			t.Errorf("alerts toolset must include %q", tool)
		}
	}
}

func TestAllows(t *testing.T) {
	var nilSet Set
	if !nilSet.Allows("any_tool") {
		t.Error("nil set must allow everything")
	}
	populated := Set{"member": {}}
	if !populated.Allows("member") {
		t.Error("populated set must allow its member")
	}
	if populated.Allows("non_member") {
		t.Error("populated set must reject non-member")
	}
}

func toSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

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

func TestParseAllWithUnknownErrors(t *testing.T) {
	_, err := Parse("all,nope")
	if err == nil {
		t.Fatal("expected error for unknown token combined with all")
	}
}

func TestParseLogsIncludesInstantQuery(t *testing.T) {
	set, err := Parse("logs")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Allows("prometheus_instant_query") {
		t.Fatal("logs toolset must include prometheus_instant_query (referenced by logjson/service_logs resources)")
	}
}

func TestParseDomainToolsetsIncludeServiceProfile(t *testing.T) {
	for _, spec := range []string{"logs", "traces", "metrics"} {
		set, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if !set.Allows("get_service_profile") {
			t.Errorf("%s toolset must include get_service_profile (profile-first firing rules on domain tools)", spec)
		}
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
	for _, want := range []string{"get_logs", "get_traces", "prometheus_instant_query", "did_you_mean", "get_service_profile", "list_datasources", "get_apm_service_deviations", "get_flamegraph", "get_profile_services"} {
		if !set.Allows(want) {
			t.Errorf("investigate missing %q", want)
		}
	}
	for _, deny := range []string{"get_alerts", "get_alert_groups", "list_dashboards", "create_dashboard", "add_drop_rule", "list_dashboard_snapshots", "validate_dashboard", "grafana_get_dashboard"} {
		if set.Allows(deny) {
			t.Errorf("investigate should exclude %q", deny)
		}
	}
}

func TestParseDashboardsIncludesValidate(t *testing.T) {
	set, err := Parse("dashboards")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Allows("validate_dashboard") || !set.Allows("get_dashboard") {
		t.Fatal("dashboards toolset missing validate_dashboard or get_dashboard")
	}
	if set.Allows("get_logs") {
		t.Fatal("dashboards should not include get_logs")
	}
}

func TestParseGrafana(t *testing.T) {
	set, err := Parse("grafana")
	if err != nil {
		t.Fatal(err)
	}
	if set == nil {
		t.Fatal("grafana must not expand to nil/all")
	}
	for _, want := range []string{"grafana_search_dashboards", "grafana_get_dashboard", "grafana_list_folders", "grafana_list_folder_dashboards", "grafana_list_datasources"} {
		if !set.Allows(want) {
			t.Errorf("grafana missing %q", want)
		}
	}
	for _, deny := range []string{"get_logs", "get_dashboard", "create_dashboard"} {
		if set.Allows(deny) {
			t.Errorf("grafana should exclude %q", deny)
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
