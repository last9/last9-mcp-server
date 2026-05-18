package dashboards

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var placeholderRE = regexp.MustCompile(`\{\{\.(\w+)\}\}`)

// RenderPlaceholders substitutes {{.KEY}} placeholders in tmpl with values from knobs.
// Returns an error if any placeholder remains unresolved or if the output is not valid JSON.
func RenderPlaceholders(tmpl string, knobs map[string]string) (string, error) {
	out := tmpl
	for k, v := range knobs {
		out = strings.ReplaceAll(out, "{{."+k+"}}", v)
	}
	if unresolved := placeholderRE.FindAllString(out, -1); len(unresolved) > 0 {
		return "", fmt.Errorf("unresolved placeholders: %v", unresolved)
	}
	if !json.Valid([]byte(out)) {
		return "", fmt.Errorf("rendered output is not valid JSON")
	}
	return out, nil
}
