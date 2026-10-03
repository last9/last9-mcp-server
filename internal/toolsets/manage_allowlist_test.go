package toolsets

import "testing"

func TestPulseManageFailsClosedForDefaultSelections(t *testing.T) {
	for _, spec := range []string{"", "all", "pulse_read", "alerts"} {
		allowed, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if ManageOnly(allowed).Allows("write_pulse_disposition") {
			t.Errorf("spec %q unexpectedly grants Pulse writes", spec)
		}
	}
}

func TestPulseManageRequiresExplicitToken(t *testing.T) {
	for _, spec := range []string{"pulse_manage", "pulse_read,pulse_manage", "all,pulse_manage"} {
		allowed, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		for _, tool := range named["pulse_manage"] {
			if !ManageOnly(allowed).Allows(tool) {
				t.Errorf("spec %q should grant %q", spec, tool)
			}
		}
	}
}

func TestPulseReadDoesNotGrantManage(t *testing.T) {
	allowed, err := Parse("pulse_read")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Allows("get_pulse_report") {
		t.Fatal("pulse_read missing get_pulse_report")
	}
	if allowed.Allows("write_pulse_disposition") {
		t.Fatal("pulse_read must not include disposition writes")
	}
}

func TestPulseToolsetsExcludeForbiddenActions(t *testing.T) {
	for _, toolset := range []string{"pulse_read", "pulse_manage"} {
		allowed, err := Parse(toolset)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"run_pulse_now", "launch_pulse_investigation", "apply_pulse_recommendation", "update_alert_rule"} {
			if allowed.Allows(forbidden) {
				t.Errorf("%s unexpectedly contains %q", toolset, forbidden)
			}
		}
	}
}
