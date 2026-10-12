package prompts

import (
	_ "embed"
	"strings"
)

//go:embed snippets/no_attributes_discovered.md
var emptyLabelsDescription string

// BuildEnhancedDescription replaces label placeholders in served descriptions.
func BuildEnhancedDescription(base string, labels []string) string {
	return buildEnhancedDescription(base, labels)
}

func buildEnhancedDescription(base string, labels []string) string {
	if !strings.Contains(base, "{{labels}}") {
		return base
	}
	if len(labels) == 0 {
		return strings.ReplaceAll(base, "{{labels}}", strings.TrimSpace(emptyLabelsDescription))
	}
	return strings.ReplaceAll(base, "{{labels}}", strings.Join(labels, ", "))
}
