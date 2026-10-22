package utils

import (
	"fmt"
	"strings"
	"testing"
)

func TestCheckPipelineRegex(t *testing.T) {
	valid := []string{
		`^[0-9]+(?:\.[0-9]+)?$`,
		`(?P<status>\d{3})`,
		`(?i)timeout|refused`,
		`checkout.*`,
	}
	for _, pattern := range valid {
		if err := CheckPipelineRegex(pattern, "q.$regex[1]"); err != nil {
			t.Errorf("CheckPipelineRegex(%q) = %v, want nil", pattern, err)
		}
	}

	invalid := []string{
		`^(?!health).*`, // negative lookahead
		`(?<=user=)\w+`, // lookbehind
		`(\w+)\s+\1`,    // backreference
		`[0-9]++`,       // possessive quantifier
		`(unclosed`,
	}
	for _, pattern := range invalid {
		err := CheckPipelineRegex(pattern, "q.$regex[1]")
		if err == nil {
			t.Errorf("CheckPipelineRegex(%q) = nil, want error", pattern)
			continue
		}
		for _, want := range []string{"q.$regex[1]", "RE2", fmt.Sprintf("%q", pattern)} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("CheckPipelineRegex(%q) error %q missing %q", pattern, err, want)
			}
		}
	}
}

func TestIsRegexFilterOperator(t *testing.T) {
	for _, op := range []string{"$regex", "$notregex", "$iregex", "$inotregex"} {
		if !IsRegexFilterOperator(op) {
			t.Errorf("IsRegexFilterOperator(%q) = false, want true", op)
		}
	}
	for _, op := range []string{"$eq", "$contains", "$replace_regex", ""} {
		if IsRegexFilterOperator(op) {
			t.Errorf("IsRegexFilterOperator(%q) = true, want false", op)
		}
	}
}
