package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const validateDashboardMaxWindow = 24 * time.Hour

var noQueryVizTypes = map[string]struct{}{
	"section":  {},
	"markdown": {},
}

// ValidateDashboardArgs is the MCP input for validate_dashboard.
type ValidateDashboardArgs struct {
	DashboardID         *string                `json:"dashboard_id,omitempty"`
	DashboardDefinition map[string]any         `json:"dashboard_definition,omitempty"`
	StartTimeISO        string                 `json:"start_time_iso"`
	EndTimeISO          string                 `json:"end_time_iso"`
	Variables           map[string]any         `json:"variables,omitempty"`
}

// GetValidateDashboardInputSchema returns the MCP-facing schema so nested
// dashboard_definition / variables are JSON objects (not byte arrays).
func GetValidateDashboardInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dashboard_id": map[string]any{
				"type":        "string",
				"description": "Saved dashboard UUID. Mutually exclusive with dashboard_definition.",
			},
			"dashboard_definition": map[string]any{
				"type":        "object",
				"description": "Inline dashboard object (name, panels[].queries[], variables). Mutually exclusive with dashboard_id. Dry run — nothing is persisted.",
			},
			"start_time_iso": map[string]any{
				"type":        "string",
				"description": "Window start, RFC3339 (required).",
			},
			"end_time_iso": map[string]any{
				"type":        "string",
				"description": "Window end, RFC3339 (required). Window must be <= 24h.",
			},
			"variables": map[string]any{
				"type":        "object",
				"description": "Optional variable values overriding dashboard defaults (scalars only).",
			},
		},
		"required": []string{"start_time_iso", "end_time_iso"},
	}
}

type validatedDashboardArgs struct {
	dashboardID *string
	definition  map[string]any
	window      map[string]string
	variables   map[string]any
}

func parseValidateISO(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp")
}

func validateDashboardInput(args ValidateDashboardArgs) (*validatedDashboardArgs, []string) {
	var errors []string
	hasID := args.DashboardID != nil && strings.TrimSpace(*args.DashboardID) != ""
	hasDefinition := args.DashboardDefinition != nil
	if hasID == hasDefinition {
		errors = append(errors, "provide exactly one of dashboard_id or dashboard_definition")
	}

	start, startErr := parseValidateISO(args.StartTimeISO)
	if startErr != nil {
		errors = append(errors, "start_time_iso is required (RFC3339)")
	}
	end, endErr := parseValidateISO(args.EndTimeISO)
	if endErr != nil {
		errors = append(errors, "end_time_iso is required (RFC3339)")
	}
	if startErr == nil && endErr == nil {
		if !end.After(start) {
			errors = append(errors, "end_time_iso must be after start_time_iso")
		} else if end.Sub(start) > validateDashboardMaxWindow {
			errors = append(errors, "window must be 24 hours or less")
		}
	}

	callerVars := args.Variables
	if callerVars == nil {
		callerVars = map[string]any{}
	}
	for key, value := range callerVars {
		switch value.(type) {
		case map[string]any, []any:
			errors = append(errors, fmt.Sprintf("variables[%q] must be a scalar", key))
		}
	}

	if len(errors) > 0 {
		return nil, errors
	}
	out := &validatedDashboardArgs{
		window: map[string]string{
			"start": args.StartTimeISO,
			"end":   args.EndTimeISO,
		},
		variables: callerVars,
	}
	if hasID {
		id := strings.TrimSpace(*args.DashboardID)
		out.dashboardID = &id
	} else {
		out.definition = args.DashboardDefinition
	}
	return out, nil
}

// NewValidateDashboardHandler returns the MCP tool handler for validate_dashboard.
func NewValidateDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ValidateDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ValidateDashboardArgs) (*mcp.CallToolResult, any, error) {
		parsed, errs := validateDashboardInput(args)
		if len(errs) > 0 {
			return nil, nil, fmt.Errorf("%s", strings.Join(errs, "; "))
		}

		exec := &validateExecutor{client: client, cfg: cfg}
		result := runValidateDashboard(ctx, exec, parsed)
		body, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal report: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
}

func runValidateDashboard(ctx context.Context, exec *validateExecutor, parsed *validatedDashboardArgs) map[string]any {
	var dashboard map[string]any
	var source string
	if parsed.dashboardID != nil {
		fetched, fetchErr := exec.fetchDashboard(ctx, *parsed.dashboardID)
		if fetchErr != "" {
			return map[string]any{
				"success": false,
				"error":   "dashboard_fetch_failed",
				"detail":  fetchErr,
			}
		}
		dashboard = fetched
		source = "saved"
	} else {
		dashboard = parsed.definition
		source = "inline"
	}

	defaults := dashboardDefaults(dashboard)
	window := parsed.window

	dashboardRef := "<inline>"
	if id, ok := dashboard["id"].(string); ok && id != "" {
		dashboardRef = id
	} else if parsed.dashboardID != nil {
		dashboardRef = *parsed.dashboardID
	}

	rawPanels := dashboard["panels"]
	var panels []any
	switch p := rawPanels.(type) {
	case nil:
		panels = []any{}
	case []any:
		panels = p
	case []map[string]any:
		panels = make([]any, len(p))
		for i, m := range p {
			panels[i] = m
		}
	default:
		return map[string]any{
			"success": false,
			"error":   "invalid_input",
			"detail":  []string{"dashboard.panels must be a list"},
		}
	}

	panelRecords := make([]map[string]any, 0, len(panels))
	for _, raw := range panels {
		panel, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					panelRecords = append(panelRecords, panelFailureRecord(panel, fmt.Errorf("%v", r)))
				}
			}()
			panelRecords = append(panelRecords, validatePanel(ctx, panel, exec, parsed.variables, defaults, window, dashboardRef))
		}()
	}

	summary := summarize(panelRecords)
	defaultsStr := map[string]string{}
	for k, v := range defaults {
		defaultsStr[k] = stringifyScalar(v)
	}

	var dashID any
	if id, ok := dashboard["id"]; ok && id != nil && id != "" {
		dashID = id
	} else if parsed.dashboardID != nil {
		dashID = *parsed.dashboardID
	} else {
		dashID = nil
	}

	out := map[string]any{
		"success":        true,
		"schema_version": schemaVersion,
		"dashboard": map[string]any{
			"source": source,
			"id":     dashID,
			"name":   dashboard["name"],
		},
		"window": window,
		"variables": map[string]any{
			"caller":             parsed.variables,
			"dashboard_defaults": defaultsStr,
		},
		"summary":   summary,
		"panels":    panelRecords,
		"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}

	// Round-trip through JSON so validateReport sees []any shapes consistently.
	encoded, err := json.Marshal(out)
	if err != nil {
		return map[string]any{"success": false, "error": "internal_error", "detail": err.Error()}
	}
	var check map[string]any
	if err := json.Unmarshal(encoded, &check); err != nil {
		return map[string]any{"success": false, "error": "internal_error", "detail": err.Error()}
	}
	if violations := validateReport(check); len(violations) > 0 {
		return map[string]any{
			"success": false,
			"error":   "internal_invalid_report",
			"detail":  violations,
		}
	}
	return check
}

func panelVizType(panel map[string]any) string {
	viz, ok := panel["visualization"].(map[string]any)
	if !ok {
		return ""
	}
	t, _ := viz["type"].(string)
	return t
}

func failureTarget(index int, err error) map[string]any {
	note := fmt.Sprintf("validator error: %T: %s", err, err.Error())
	if len(note) > 200 {
		note = note[:200]
	}
	return map[string]any{
		"target_index": index,
		"query_type":   "unknown",
		"status":       "execution_error",
		"note":         note,
		"lint":         []any{},
		"execution":    nil,
		"evidence":     nil,
		"diagnosis":    nil,
	}
}

func panelFailureRecord(panel map[string]any, err error) map[string]any {
	name, _ := panel["name"].(string)
	return map[string]any{
		"panel_id":           panel["id"],
		"name":               name,
		"visualization_type": panelVizType(panel),
		"classification":     "query",
		"targets":            []map[string]any{failureTarget(0, err)},
	}
}

func detectQueryType(query map[string]any) string {
	if qt, ok := query["query_type"].(string); ok {
		if isQueryType(qt) {
			return qt
		}
		if qt != "" {
			return "unknown"
		}
	}
	if telemetry, _ := query["telemetry"].(string); telemetry == "metrics" {
		return "promql"
	}
	return "unknown"
}

func validatePanel(
	ctx context.Context,
	panel map[string]any,
	exec *validateExecutor,
	callerVars, defaults map[string]any,
	window map[string]string,
	dashboardRef string,
) map[string]any {
	_ = dashboardRef
	vizType := panelVizType(panel)
	record := map[string]any{
		"panel_id":           panel["id"],
		"name":               panel["name"],
		"visualization_type": nilIfEmpty(vizType),
		"classification":     "query",
		"targets":            []map[string]any{},
	}

	queries := asAnySlice(panel["queries"])
	_, isCSV := panel["csv"]
	if _, noQuery := noQueryVizTypes[vizType]; noQuery || isCSV || len(queries) == 0 {
		record["classification"] = "no_query"
		reason := "no_queries_defined"
		if isCSV {
			reason = "csv_panel"
		} else if _, ok := noQueryVizTypes[vizType]; ok {
			reason = vizType + "_panel"
		}
		record["reason"] = reason
		return record
	}

	targets := make([]map[string]any, 0, len(queries))
	for index, raw := range queries {
		query, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					targets = append(targets, failureTarget(index, fmt.Errorf("%v", r)))
				}
			}()
			targets = append(targets, validateTarget(ctx, query, index, vizType, exec, callerVars, defaults, window))
		}()
	}
	if len(targets) == 0 {
		record["classification"] = "no_query"
		record["reason"] = "no_queries_defined"
	} else {
		record["targets"] = targets
	}
	return record
}

func validateTarget(
	ctx context.Context,
	query map[string]any,
	index int,
	vizType string,
	exec *validateExecutor,
	callerVars, defaults map[string]any,
	window map[string]string,
) map[string]any {
	queryType := detectQueryType(query)
	expr := query["expr"]
	target := map[string]any{
		"target_index": index,
		"query_name":   query["name"],
		"query_type":   queryType,
		"telemetry":    query["telemetry"],
		"lint":         []any{},
		"execution":    nil,
		"evidence":     nil,
		"diagnosis":    nil,
	}

	var resolved any
	var used map[string]map[string]any
	var unresolved []string

	if queryType == "log_json" || queryType == "trace_json" {
		pipeline := parsePipeline(expr)
		if pipeline != nil {
			resolved, used, unresolved = interpolatePipeline(pipeline, callerVars, defaults)
		} else {
			resolved, used, unresolved = interpolatePipeline(expr, callerVars, defaults)
		}
	} else {
		exprStr, _ := expr.(string)
		var s string
		s, used, unresolved = interpolate(exprStr, callerVars, defaults)
		resolved = s
	}
	if len(used) > 0 {
		target["variables_used"] = used
	}
	if len(unresolved) > 0 {
		target["status"] = "unresolved_variable"
		target["unresolved_variables"] = unresolved
		return target
	}

	findings := lintTarget(queryType, resolved, vizType)
	lintAny := make([]any, len(findings))
	for i, f := range findings {
		lintAny[i] = f
	}
	target["lint"] = lintAny
	if hasBlockingError(findings) {
		target["status"] = "invalid_query"
		return target
	}

	var outcome executionOutcome
	switch queryType {
	case "promql":
		exprStr, _ := resolved.(string)
		outcome = exec.runPromQL(ctx, exprStr, window, vizType)
	case "log_json":
		indexName, _ := query["index_name"].(string)
		outcome = exec.runLogJSON(ctx, resolved, window, indexName)
	default:
		target["status"] = "unsupported_execution"
		target["note"] = queryType + " execution is not supported; lint-only"
		return target
	}

	target["execution"] = map[string]any{
		"tool":        outcome.tool,
		"duration_ms": outcome.durationMs,
		"error":       nilIfEmpty(outcome.errorText),
	}
	if outcome.status != "executed" {
		target["status"] = outcome.status
		return target
	}

	target["evidence"] = boundedEvidence(outcome.payload)
	if hasData(outcome.payload) {
		target["status"] = "valid_with_data"
	} else {
		// Day-1: no diagnose probes (ENG-1915 / U6).
		target["status"] = "valid_no_data"
		target["diagnosis"] = nil
	}
	return target
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
