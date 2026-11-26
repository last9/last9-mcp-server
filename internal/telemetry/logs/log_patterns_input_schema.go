package logs

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// GetLogPatternsInputSchema is the schema the SDK would infer from
// GetLogPatternsArgs, served explicitly like its emerging twin so the two
// tools' shared scope cannot drift apart if inference rules change. Time
// fields carry format date-time, which inference cannot express.
func GetLogPatternsInputSchema() *jsonschema.Schema {
	schema := inferInputSchema[GetLogPatternsArgs]("get_log_patterns")
	markDateTime(schema, "start_time_iso", "end_time_iso")
	return schema
}

// GetEmergingLogPatternsInputSchema is GetLogPatternsInputSchema's
// counterpart for GetEmergingLogPatternsArgs, plus the baseline time pair
// that must be set together, as get_apm_service_deviations declares it.
func GetEmergingLogPatternsInputSchema() *jsonschema.Schema {
	schema := inferInputSchema[GetEmergingLogPatternsArgs]("get_emerging_log_patterns")
	markDateTime(schema, "start_time_iso", "end_time_iso", "baseline_start_time_iso", "baseline_end_time_iso")
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

func markDateTime(schema *jsonschema.Schema, properties ...string) {
	for _, name := range properties {
		property, ok := schema.Properties[name]
		if !ok {
			// A renamed field must not silently lose its format.
			panic(fmt.Sprintf("input schema has no property %q", name))
		}
		property.Format = "date-time"
	}
}
