package guardrail

import "testing"

func TestParseMode(t *testing.T) {
	valid := map[string]string{"": ModeOff, "off": ModeOff, " Shadow ": ModeShadow, "ENFORCE": ModeEnforce}
	for in, want := range valid {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"on", "block", "warn"} {
		if _, err := ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) should fail", in)
		}
	}
}
