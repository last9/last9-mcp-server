package main

import (
	"strconv"
	"strings"
	"testing"

	"last9-mcp/internal/utils"
)

// lookbackMaxExemptTools serve lookback_minutes without stating MaxLookbackMinutes:
// each enforces a narrower cap of its own, or has eval-sensitive copy.
var lookbackMaxExemptTools = map[string]string{
	"get_alerts":                     "range 1-60",
	"get_databases":                  "maximum window 7 days",
	"get_trace_attribute_deviations": "maximum 15",
	"prometheus_instant_query":       "eval-sensitive",
	"prometheus_label_values":        "eval-sensitive",
	"prometheus_labels":              "eval-sensitive",
	"prometheus_range_query":         "eval-sensitive",
}

func TestServedLookbackDescriptionsStateMaximum(t *testing.T) {
	tools, _ := servedSurface(t)
	maxText := strconv.Itoa(utils.MaxLookbackMinutes)
	served := map[string]bool{}
	for _, tool := range tools {
		schema, _ := schemaAsAny(tool.InputSchema).(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		lookback, ok := props["lookback_minutes"].(map[string]any)
		if !ok {
			continue
		}
		served[tool.Name] = true
		if _, exempt := lookbackMaxExemptTools[tool.Name]; exempt {
			continue
		}
		if desc, _ := lookback["description"].(string); !strings.Contains(desc, maxText) {
			t.Errorf("%s: lookback_minutes description %q does not state the maximum %s", tool.Name, desc, maxText)
		}
	}
	for name := range lookbackMaxExemptTools {
		if !served[name] {
			t.Errorf("lookbackMaxExemptTools names %q, which serves no lookback_minutes", name)
		}
	}
}
