package logs

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// GetLogPatternsInputSchema is the schema the SDK would infer from
// GetLogPatternsArgs, served explicitly like its emerging twin so the two
// tools' shared scope cannot drift apart if inference rules change.
func GetLogPatternsInputSchema() *jsonschema.Schema {
	return inferInputSchema[GetLogPatternsArgs]("get_log_patterns")
}

// GetEmergingLogPatternsInputSchema is the schema the SDK would infer from
// GetEmergingLogPatternsArgs, plus the baseline time pair that must be set
// together, as get_apm_service_deviations declares it.
func GetEmergingLogPatternsInputSchema() *jsonschema.Schema {
	schema := inferInputSchema[GetEmergingLogPatternsArgs]("get_emerging_log_patterns")
	schema.DependentRequired = map[string][]string{
		"baseline_start_time_iso": {"baseline_end_time_iso"},
		"baseline_end_time_iso":   {"baseline_start_time_iso"},
	}
	return schema
}

func inferInputSchema[T any](tool string) *jsonschema.Schema {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{})
	if err != nil {
		// Like mcp.AddTool's own inference: a struct that cannot be described is a programming error.
		panic(fmt.Sprintf("%s input schema: %v", tool, err))
	}
	return schema
}
