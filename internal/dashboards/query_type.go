package dashboards

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var queryTypesByTelemetry = map[string][]string{
	"metrics": {"promql"},
	"logs":    {"log_json", "log_ql", "log_raw"},
	"traces":  {"trace_json", "trace_ql"},
}

var allQueryTypes = []string{"promql", "log_json", "log_ql", "log_raw", "trace_json", "trace_ql"}

// checkDashboardQueryTypes leaves malformed panels and queries lists to the API;
// it only rejects a query_type the API would refuse.
func checkDashboardQueryTypes(dashboard map[string]any) error {
	panels, _ := dashboard["panels"].([]any)
	for i, raw := range panels {
		panel, _ := raw.(map[string]any)
		if err := checkQueriesQueryTypes(panel["queries"]); err != nil {
			return fmt.Errorf("panels[%d].%w", i, err)
		}
	}
	return nil
}

func checkQueriesQueryTypes(queries any) error {
	list, _ := queries.([]any)
	for i, raw := range list {
		query, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("queries[%d] must be an object", i)
		}
		if err := checkQueryType(query); err != nil {
			return fmt.Errorf("queries[%d]: %w", i, err)
		}
	}
	return nil
}

// checkQueryType accepts an absent query_type: the API, not this check, requires it.
// An unrecognised telemetry is checked against every query type, so it is never
// stricter than the API.
func checkQueryType(query map[string]any) error {
	raw, present := query["query_type"]
	if !present {
		return nil
	}
	telemetry, _ := query["telemetry"].(string)
	valid, known := queryTypesByTelemetry[telemetry]
	if !known {
		valid = allQueryTypes
	}
	use := "use " + joinOr(valid)
	queryType, ok := raw.(string)
	switch {
	case !ok:
		return errors.New("query_type must be a string; " + use)
	case slices.Contains(valid, queryType):
		return nil
	case known:
		return fmt.Errorf("query_type '%s' is not valid for telemetry %s; %s", queryType, telemetry, use)
	default:
		return fmt.Errorf("query_type '%s' is not valid; %s", queryType, use)
	}
}

func joinOr(values []string) string {
	if len(values) == 1 {
		return values[0]
	}
	return strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
}
