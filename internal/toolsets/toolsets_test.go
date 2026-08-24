package toolsets

import (
	"sort"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	investigate := func() map[string]bool {
		want := map[string]bool{}
		for _, domain := range []string{"logs", "traces", "metrics"} {
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
