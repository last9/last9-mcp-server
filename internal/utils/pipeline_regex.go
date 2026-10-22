package utils

import (
	"fmt"
	"regexp"
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
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf(
			"invalid regexp %q at %s: %v — patterns use RE2 syntax: no lookahead/lookbehind ((?=, (?!, (?<=, (?<!), backreferences (\\1) or possessive quantifiers (++); negate with $notregex instead of (?!…)",
			pattern, path, err,
		)
	}
	return nil
}
