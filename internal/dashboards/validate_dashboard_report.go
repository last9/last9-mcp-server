package dashboards

import "strconv"

// Report schema constants + fail-closed self-validation (dashboard_validation/v1).
// Port of agents/supervisor/skills/dashboard_validation/report.py.

const schemaVersion = "dashboard_validation/v1"

var targetStatuses = map[string]struct{}{
	"valid_with_data":       {},
	"valid_no_data":         {},
	"invalid_query":         {},
	"execution_error":       {},
	"source_unavailable":    {},
	"unsupported_execution": {},
	"unresolved_variable":   {},
}

var panelClassifications = map[string]struct{}{
	"query":    {},
	"no_query": {},
}

var diagnosisKinds = map[string]struct{}{
	"metric_absent":            {},
	"label_value_absent":       {},
	"log_attribute_unpopulated": {},
	"window_empty":             {},
	"unknown":                  {},
}

var queryTypes = map[string]struct{}{
	"promql":     {},
	"log_json":   {},
	"log_ql":     {},
	"trace_json": {},
	"trace_ql":   {},
	"unknown":    {},
}

func isQueryType(s string) bool {
	_, ok := queryTypes[s]
	return ok
}

// validateReport returns schema violations (empty means valid). Fail-closed:
// unknown statuses/kinds, missing evidence on diagnoses, or count drift.
func validateReport(report map[string]any) []string {
	var errors []string
	if report == nil {
		return []string{"report must be a dict"}
	}
	if report["schema_version"] != schemaVersion {
		errors = append(errors, "schema_version must be "+schemaVersion)
	}
	for _, key := range []string{"dashboard", "window", "summary", "panels"} {
		if _, ok := report[key]; !ok {
			errors = append(errors, "missing key: "+key)
		}
	}
	if len(errors) > 0 {
		return errors
	}

	panels, ok := report["panels"].([]any)
	if !ok {
		// Also accept []map from internal assembly before marshal round-trip.
		if typed, ok := report["panels"].([]map[string]any); ok {
			panels = make([]any, len(typed))
			for i, p := range typed {
				panels[i] = p
			}
		} else {
			return []string{"panels must be a list"}
		}
	}

	targetCount := 0
	byStatus := map[string]int{}
	for i, raw := range panels {
		panel, ok := raw.(map[string]any)
		if !ok {
			errors = append(errors, "panels["+strconv.Itoa(i)+"] must be a dict")
			continue
		}
		classification, _ := panel["classification"].(string)
		if _, ok := panelClassifications[classification]; !ok {
			errors = append(errors, "panels["+strconv.Itoa(i)+"].classification invalid: "+classification)
		}
		targets := asAnySlice(panel["targets"])
		if classification == "no_query" && len(targets) > 0 {
			errors = append(errors, "panels["+strconv.Itoa(i)+"] is no_query but has targets")
		}
		for j, traw := range targets {
			targetCount++
			where := "panels[" + strconv.Itoa(i) + "].targets[" + strconv.Itoa(j) + "]"
			target, ok := traw.(map[string]any)
			if !ok {
				errors = append(errors, where+" must be a dict")
				continue
			}
			status, _ := target["status"].(string)
			if _, ok := targetStatuses[status]; !ok {
				errors = append(errors, where+".status invalid: "+status)
				continue
			}
			byStatus[status]++
			qt, _ := target["query_type"].(string)
			if !isQueryType(qt) {
				errors = append(errors, where+".query_type invalid: "+qt)
			}
			if status == "valid_with_data" || status == "valid_no_data" {
				if _, ok := target["evidence"].(map[string]any); !ok {
					errors = append(errors, where+".evidence required for status "+status)
				}
			}
			if diagnosis := target["diagnosis"]; diagnosis != nil {
				errors = append(errors, validateDiagnosis(diagnosis, where)...)
			}
		}
	}

	summary, _ := report["summary"].(map[string]any)
	if summary == nil {
		errors = append(errors, "summary must be a dict")
		return errors
	}
	targetsTotal := asInt(summary["targets_total"])
	if targetsTotal != targetCount {
		errors = append(errors, "summary.targets_total="+strconv.Itoa(targetsTotal)+" but counted "+strconv.Itoa(targetCount))
	}
	declared := map[string]int{}
	if raw, ok := summary["by_status"].(map[string]any); ok {
		for k, v := range raw {
			declared[k] = asInt(v)
		}
	} else if typed, ok := summary["by_status"].(map[string]int); ok {
		declared = typed
	}
	for status, count := range byStatus {
		if declared[status] != count {
			errors = append(errors, "summary.by_status["+status+"]="+strconv.Itoa(declared[status])+" but counted "+strconv.Itoa(count))
		}
	}
	for status, count := range declared {
		if count > 0 {
			if _, ok := byStatus[status]; !ok {
				errors = append(errors, "summary.by_status["+status+"]="+strconv.Itoa(count)+" but no such targets")
			}
		}
	}
	return errors
}

func validateDiagnosis(diagnosis any, where string) []string {
	d, ok := diagnosis.(map[string]any)
	if !ok {
		return []string{where + ".diagnosis must be a dict"}
	}
	possibilities := asAnySlice(d["possibilities"])
	if len(possibilities) == 0 {
		return []string{where + ".diagnosis.possibilities must be a non-empty list"}
	}
	var errors []string
	for k, raw := range possibilities {
		poss, ok := raw.(map[string]any)
		if !ok {
			errors = append(errors, where+".diagnosis.possibilities["+strconv.Itoa(k)+"].kind invalid: <non-object>")
			continue
		}
		kind, _ := poss["kind"].(string)
		if _, ok := diagnosisKinds[kind]; !ok {
			errors = append(errors, where+".diagnosis.possibilities["+strconv.Itoa(k)+"].kind invalid: "+kind)
			continue
		}
		if kind != "unknown" {
			evidence := asAnySlice(poss["evidence"])
			if len(evidence) == 0 {
				errors = append(errors, where+".diagnosis.possibilities["+strconv.Itoa(k)+"] kind="+kind+" lacks probe evidence")
			}
		}
	}
	return errors
}

// summarize builds the summary block from assembled panel records.
func summarize(panels []map[string]any) map[string]any {
	byStatus := map[string]int{}
	targetsTotal := 0
	panelsNoQuery := 0
	for _, panel := range panels {
		if panel["classification"] == "no_query" {
			panelsNoQuery++
		}
		for _, traw := range asAnySlice(panel["targets"]) {
			targetsTotal++
			target, _ := traw.(map[string]any)
			status, _ := target["status"].(string)
			if status == "" {
				status = "execution_error"
			}
			byStatus[status]++
		}
	}

	overall := "pass"
	for _, s := range []string{"invalid_query", "execution_error", "source_unavailable"} {
		if byStatus[s] > 0 {
			overall = "fail"
			break
		}
	}
	if overall == "pass" {
		for _, s := range []string{"unsupported_execution", "unresolved_variable"} {
			if byStatus[s] > 0 {
				overall = "partial"
				break
			}
		}
	}

	return map[string]any{
		"panels_total":    len(panels),
		"panels_no_query": panelsNoQuery,
		"targets_total":   targetsTotal,
		"by_status":       byStatus,
		"overall":         overall,
	}
}

func asAnySlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []map[string]any:
		out := make([]any, len(t))
		for i, m := range t {
			out[i] = m
		}
		return out
	default:
		return nil
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}
