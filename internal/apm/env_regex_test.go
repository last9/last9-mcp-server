package apm

import (
	"regexp"
	"strconv"
	"testing"
)

var envMatcherRE = regexp.MustCompile(`env(=~|=)("(?:[^"\\]|\\.)*")`)

func assertEnvMatcherBehavior(t *testing.T, query, wantMatch, wantReject string) {
	t.Helper()
	matches := envMatcherRE.FindAllStringSubmatch(query, -1)
	if len(matches) == 0 {
		t.Fatalf("no env matcher found in query: %s", query)
	}
	for _, m := range matches {
		op, quoted := m[1], m[2]
		value, err := strconv.Unquote(quoted)
		if err != nil {
			t.Fatalf("failed to unquote env matcher value %q: %v", quoted, err)
		}
		if op == "=" {
			if value != wantMatch {
				t.Errorf("literal env matcher %q, want %q", value, wantMatch)
			}
			continue
		}
		re, err := regexp.Compile("^(?:" + value + ")$")
		if err != nil {
			t.Fatalf("env regex %q does not compile: %v", value, err)
		}
		if !re.MatchString(wantMatch) {
			t.Errorf("env regex %q did not match %q", value, wantMatch)
		}
		if wantReject != "" && re.MatchString(wantReject) {
			t.Errorf("env regex %q incorrectly matched %q", value, wantReject)
		}
	}
}
