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
