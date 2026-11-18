package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

const (
	previewSampleBytes = 1024
	previewResultBytes = 256 << 10
)

func previewPanels(ctx context.Context, exec *validateExecutor, parsed *validatedDashboardArgs) (map[string]any, error) {
	executor := *exec
	executor.preview = true
	report := runValidateDashboard(ctx, &executor, parsed)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if report["success"] != true {
		return nil, fmt.Errorf("dashboard validation failed: %v", report["detail"])
	}
	panels := asAnySlice(report["panels"])
	for index, raw := range panels {
		panel := raw.(map[string]any)
		panel["status"] = previewPanelStatus(panel)
		definition := asAnySlice(parsed.definition["panels"])[index]
		panelDefinition, _ := definition.(map[string]any)
		if err := errors.Join(validatePreviewSchema(definition, panelObjectSchema()), checkQueriesQueryTypes(panelDefinition["queries"])); err != nil {
			panel["status"] = "error"
			panel["definition_error"] = err.Error()
		}
	}
	validation := report["summary"].(map[string]any)
	validation["definition_error"] = nil
	if err := errors.Join(validatePreviewSchema(parsed.definition, dashboardObjectSchema("")), checkDashboardQueryTypes(parsed.definition)); err != nil {
		validation["overall"] = "fail"
		validation["definition_error"] = err.Error()
	}
	return map[string]any{
		"validation": validation, "panels": panels, "window": report["window"],
		"saved": false,
		"note":  "Client-side preview only. No dashboard is saved. Unsupported query types are panel errors. Backend validation, assigned IDs, defaults and concurrent edits may change the saved result.",
	}, nil
}

func previewPanelStatus(panel map[string]any) string {
	if panel["classification"] == "no_query" {
		return "ok"
	}
	status := "empty"
	targets := asAnySlice(panel["targets"])
	if len(targets) == 0 {
		return "error"
	}
	for _, raw := range targets {
		target, ok := raw.(map[string]any)
		if !ok {
			return "error"
		}
		switch target["status"] {
		case "valid_with_data":
			status = "ok"
		case "valid_no_data":
		default:
			return "error"
		}
	}
	return status
}

func boundPreviewEvidence(target map[string]any) {
	evidence, ok := target["evidence"].(map[string]any)
	if !ok {
		return
	}
	samples := asAnySlice(evidence["samples"])
	kept := make([]any, 0, len(samples))
	remaining := previewSampleBytes - 2
	for _, sample := range samples {
		raw, err := json.Marshal(sample)
		size := len(raw)
		if len(kept) > 0 {
			size++
		}
		if err != nil || size > remaining {
			break
		}
		kept = append(kept, sample)
		remaining -= size
	}
	if len(kept) < len(samples) {
		evidence["samples"] = kept
		evidence["sample_count"] = len(kept)
		evidence["truncated"] = true
		evidence["note"] = "Samples omitted to fit the 1024-byte per-target budget."
	}
}

func previewResult(report map[string]any) ([]byte, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if len(raw) > previewResultBytes {
		return nil, fmt.Errorf("preview exceeds %d bytes; use a smaller dashboard or shorter queries and variable values", previewResultBytes)
	}
	return raw, nil
}

func validatePreviewSchema(value any, definition map[string]any) error {
	if dashboard, ok := value.(map[string]any); ok {
		properties, _ := definition["properties"].(map[string]any)
		if _, isDashboard := properties["panels"]; isDashboard {
			normalizeDashboardShape(dashboard)
		} else {
			normalizeDashboardShape(map[string]any{"panels": []any{dashboard}})
		}
	}
	raw, err := json.Marshal(definition)
	if err != nil {
		return err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	return resolved.Validate(value)
}
