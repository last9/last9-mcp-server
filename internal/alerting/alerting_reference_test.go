package alerting

import (
	"regexp"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

// Each alerting.md template must validate against validateAlertRulePayload: a
// primary_indicator that is not a key in its own expression_args stalls
// create_alert for anyone who copies the template verbatim.
func TestAlertingReferenceTemplatesValidate(t *testing.T) {
	templateHeading := regexp.MustCompile(`(?m)^### .+$`)
	primaryIndicator := regexp.MustCompile(`(?m)^primary_indicator: (\w+)$`)
	expressionArgBinding := regexp.MustCompile(`(?m)^\s*"?(\w+)"?:\s*\{\s*(?:"?id"?|id):\s*"<[^"]+>"`)

	headings := templateHeading.FindAllStringIndex(prompts.AlertingReference, -1)
	for i, loc := range headings {
		end := len(prompts.AlertingReference)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		section := prompts.AlertingReference[loc[0]:end]
		title := strings.TrimPrefix(strings.Split(section, "\n")[0], "### ")

		primaryMatch := primaryIndicator.FindStringSubmatch(section)
		if primaryMatch == nil {
			continue
		}
		bindings := map[string]AlertExpressionArg{}
		for _, m := range expressionArgBinding.FindAllStringSubmatch(section, -1) {
			bindings[m[1]] = AlertExpressionArg{ID: "kpi-uuid"}
		}
		if len(bindings) == 0 {
			continue
		}

		rule := AlertRulePayload{PrimaryIndicator: primaryMatch[1], ExpressionArgs: bindings}
		if err := validateAlertRulePayload(rule); err != nil {
			t.Errorf("template %q: %v", title, err)
		}
	}
}
