package logs

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// GetEmergingLogPatternsInputSchema is the schema the SDK would infer from
// GetEmergingLogPatternsArgs, plus the baseline time pair that must be set
// together, as get_apm_service_deviations declares it.
func GetEmergingLogPatternsInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[GetEmergingLogPatternsArgs](&jsonschema.ForOptions{})
	if err != nil {
		// Like mcp.AddTool's own inference: a struct that cannot be described is a programming error.
		panic(fmt.Sprintf("get_emerging_log_patterns input schema: %v", err))
	}
	schema.DependentRequired = map[string][]string{
		"baseline_start_time_iso": {"baseline_end_time_iso"},
		"baseline_end_time_iso":   {"baseline_start_time_iso"},
	}
	return schema
}
