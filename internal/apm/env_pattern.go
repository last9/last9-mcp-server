package apm

import (
	"fmt"
	"regexp"
	"strings"
)

// literalEnvPattern converts a tool's env argument into the regex fragment
// placed in a PromQL env=~"..." matcher, plus the normalized literal
// environment name underneath it. These tools match one environment name
// exactly, but models copy get_service_summary's documented regex style
// (^prod$), which a plain QuoteMeta would turn into a pattern that matches
// nothing. So: ""/".*"/"^.*$" mean all environments, a full ^...$ anchor
// wrapper is stripped, and the rest is quoted literally. Only unmistakable
// regex intent (an alternation "|" or a ".*" / ".+" wildcard) is rejected;
// brackets, "?", backslashes etc. stay literal so names like prod[blue] still
// match themselves exactly.
//
// literal is the normalized exact environment name, meant for exact-match
// callers such as deeplink.APMCatalogEnvExact — never the escaped pattern,
// since dotted/bracketed names must stay literal rather than regex-escaped.
// literal is "" for the all-environments case (pattern ".*"). display is the
// normalized value tools echo back: literal, or ".*" for all environments.
func envRequested(raw, display string) string {
	if raw == display {
		return ""
	}
	return raw
}

// envLiteral maps a literalEnvPattern display value back to its literal:
// "" for all environments.
func envLiteral(display string) string {
	if display == ".*" {
		return ""
	}
	return display
}

func literalEnvPattern(env string) (pattern, literal, display string, err error) {
	lit := env
	if strings.HasPrefix(lit, "^") && strings.HasSuffix(lit, "$") && len(lit) >= 2 {
		lit = lit[1 : len(lit)-1]
		if strings.HasPrefix(lit, "(?:") && strings.HasSuffix(lit, ")") {
			lit = lit[3 : len(lit)-1]
		} else if strings.HasPrefix(lit, "(") && strings.HasSuffix(lit, ")") {
			lit = lit[1 : len(lit)-1]
		}
	}
	if lit == ".*" || (env == "" && lit == "") {
		return ".*", "", ".*", nil
	}
	if lit == "" || strings.Contains(lit, "|") || strings.Contains(lit, ".*") || strings.Contains(lit, ".+") {
		return "", "", "", fmt.Errorf("env %q looks like a regular expression; this tool matches one environment name exactly (for example \"production\"). Use get_service_environments to list names.", env)
	}
	return regexp.QuoteMeta(lit), lit, lit, nil
}
