package utils

import (
	"fmt"
	"regexp"
	"strings"
)

var regexFilterOperators = map[string]struct{}{
	"$regex":     {},
	"$notregex":  {},
	"$iregex":    {},
	"$inotregex": {},
}

// IsRegexFilterOperator reports whether a logjson/tracejson filter operator
// takes a regular expression as its pattern argument.
func IsRegexFilterOperator(operator string) bool {
	_, ok := regexFilterOperators[operator]
	return ok
}

// CheckPipelineRegex compiles a pipeline regex locally so a pattern the backend
// would reject fails with the compile error instead of an opaque upstream 400.
// Go's regexp implements the RE2 syntax the backend accepts.
func CheckPipelineRegex(pattern, path string) error {
	if _, err := regexp.Compile(goCompatibleEscapes(pattern)); err != nil {
		return fmt.Errorf(
			"invalid regexp %q at %s: %v — patterns use RE2 syntax: no lookahead/lookbehind ((?=, (?!, (?<=, (?<!), backreferences (\\1) or possessive quantifiers (++); negate with $notregex instead of (?!…)",
			pattern, path, err,
		)
	}
	return nil
}

// goCompatibleEscapes rewrites \C (any byte) and \e (ESC), which the backend's
// RE2 accepts but Go's regexp rejects. The result is only compiled to validate
// the pattern; the original is what goes upstream.
func goCompatibleEscapes(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '\\' || i+1 == len(pattern) {
			b.WriteByte(pattern[i])
			continue
		}
		i++
		switch pattern[i] {
		case 'C':
			b.WriteString(`(?s:.)`)
		case 'e':
			b.WriteString(`\x1b`)
		default:
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}
