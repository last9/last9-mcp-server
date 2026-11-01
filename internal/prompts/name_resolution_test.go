package prompts_test

import (
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

func TestServiceDescriptionsExplainNameResolution(t *testing.T) {
	for name, description := range map[string]string{"get_logs": prompts.GetLogsDescription, "get_traces": prompts.GetTracesDescription, "get_service_logs": prompts.GetServiceLogsDescription} {
		for _, phrase := range []string{"closest match", "candidates", "no measurements"} {
			if !strings.Contains(description, phrase) {
				t.Errorf("%s omits %q", name, phrase)
			}
		}
	}
}

func TestMetricDescriptionsExplainNameResolution(t *testing.T) {
	for name, description := range map[string]string{"instant": prompts.PromqlInstantQueryDetails, "range": prompts.PromqlRangeQueryDetails, "status": prompts.GetMetricStatusDescription} {
		for _, phrase := range []string{"single metric selector", "closest match", "candidates", "Expressions are unchanged"} {
			if !strings.Contains(description, phrase) {
				t.Errorf("%s omits %q", name, phrase)
			}
		}
	}
}

func TestLabelDescriptionsExplainNameResolution(t *testing.T) {
	for name, description := range map[string]string{"labels": prompts.PromqlLabelsQueryDetails, "values": prompts.PromqlLabelValuesQueryDetails} {
		for _, phrase := range []string{"Empty lookups", "closest match", "no measurements"} {
			if !strings.Contains(description, phrase) {
				t.Errorf("%s omits %q", name, phrase)
			}
		}
	}
}
