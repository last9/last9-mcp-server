package dashboards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxPatchDiffChars = 2000
	maxDiffValueChars = 200
)

var knownPatchOps = map[string]bool{
	"add_panel": true, "update_panel": true, "remove_panel": true,
	"move_panel": true, "set_title": true, "set_variable": true,
}

type PatchDashboardArgs struct {
	ID  string             `json:"id" jsonschema:"(Required) Dashboard UUID"`
	Ops []PatchDashboardOp `json:"ops" jsonschema:"(Required) Ops applied in order; if any op is invalid nothing is written"`
}

type PatchDashboardOp struct {
	Op       string         `json:"op" jsonschema:"(Required) add_panel, update_panel, remove_panel, move_panel, set_title or set_variable"`
	PanelID  string         `json:"panel_id,omitempty" jsonschema:"Panel id (update_panel, remove_panel, move_panel)"`
	Panel    map[string]any `json:"panel,omitempty" jsonschema:"Full panel object, same shape as in create_dashboard (add_panel)"`
	Fields   map[string]any `json:"fields,omitempty" jsonschema:"Top-level panel fields to replace; null removes a field (update_panel)"`
	Index    *int           `json:"index,omitempty" jsonschema:"0-based position in panels: insert position for add_panel (default: append), target position for move_panel (required)"`
	Title    string         `json:"title,omitempty" jsonschema:"New dashboard name (set_title)"`
	Variable map[string]any `json:"variable,omitempty" jsonschema:"Variable object; replaces the variable with the same target, else is appended (set_variable)"`
}

type patchChange struct {
	Op      string                 `json:"op"`
	PanelID string                 `json:"panel_id,omitempty"`
	Name    string                 `json:"name,omitempty"`
	Index   *int                   `json:"index,omitempty"`
	From    *int                   `json:"from,omitempty"`
	To      *int                   `json:"to,omitempty"`
	Target  string                 `json:"target,omitempty"`
	Action  string                 `json:"action,omitempty"`
	Before  json.RawMessage        `json:"before,omitempty"`
	After   json.RawMessage        `json:"after,omitempty"`
	Fields  map[string]fieldChange `json:"fields,omitempty"`
}

type fieldChange struct {
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type patchDiff struct {
	DashboardID string        `json:"dashboard_id"`
	Changes     []patchChange `json:"changes"`
	Note        string        `json:"note,omitempty"`
}

func NewPatchDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PatchDashboardArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args PatchDashboardArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}
		if err := validatePatchOps(args.Ops); err != nil {
			return nil, nil, err
		}
		region, err := resolveRegion(cfg, "")
		if err != nil {
			return nil, nil, err
		}

		u := cfg.APIBaseURL + fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(args.ID))
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodGet, u+"?"+url.Values{"region": {region}}.Encode(), nil)
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}
		var current struct {
			Dashboard map[string]any  `json:"dashboard"`
			Metadata  json.RawMessage `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(body, &current); err != nil || current.Dashboard == nil {
			return nil, nil, errors.New("get dashboard returned no dashboard object; nothing was written")
		}

		changes, err := applyPatchOps(current.Dashboard, args.Ops)
		if err != nil {
			return nil, nil, err
		}

		dashboard, err := json.Marshal(current.Dashboard)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal dashboard: %w", err)
		}
		payload, err := marshalDashboardRequest(DashboardRequest{Dashboard: dashboard, Metadata: current.Metadata})
		if err != nil {
			return nil, nil, err
		}
		if _, _, err := doJSONRequest(ctx, client, cfg, http.MethodPut, u, payload); err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: renderPatchDiff(patchDiff{DashboardID: args.ID, Changes: changes})}},
			Meta:    deeplink.ToMeta(dlBuilder.BuildDashboardLink(args.ID)),
		}, nil, nil
	}
}

// validatePatchOps checks what needs no dashboard, so a malformed op costs no API call.
func validatePatchOps(ops []PatchDashboardOp) error {
	if len(ops) == 0 {
		return errors.New("ops must contain at least one op")
	}
	for i, op := range ops {
		if err := validatePatchOp(op); err != nil {
			return opError(i, op, err)
		}
	}
	return nil
}

func validatePatchOp(op PatchDashboardOp) error {
	switch op.Op {
	case "add_panel":
		if op.Panel == nil {
			return errors.New("panel is required")
		}
	case "update_panel":
		if op.PanelID == "" {
			return errors.New("panel_id is required")
		}
		if len(op.Fields) == 0 {
			return errors.New("fields is required")
		}
		if _, ok := op.Fields["id"]; ok {
			return errors.New("fields must not change id; remove_panel then add_panel instead")
		}
	case "remove_panel":
		if op.PanelID == "" {
			return errors.New("panel_id is required")
		}
	case "move_panel":
		if op.PanelID == "" {
			return errors.New("panel_id is required")
		}
		if op.Index == nil {
			return errors.New("index is required")
		}
	case "set_title":
		if strings.TrimSpace(op.Title) == "" {
			return errors.New("title is required")
		}
	case "set_variable":
		if target, _ := op.Variable["target"].(string); target == "" {
			return errors.New("variable.target is required")
		}
	default:
		return fmt.Errorf("unknown op %q; use add_panel, update_panel, remove_panel, move_panel, set_title or set_variable", op.Op)
	}
	return nil
}

func opError(i int, op PatchDashboardOp, err error) error {
	prefix := fmt.Sprintf("ops[%d]", i)
	if knownPatchOps[op.Op] {
		prefix += " (" + op.Op + ")"
	}
	return fmt.Errorf("%s: %w; nothing was written", prefix, err)
}

func applyPatchOps(doc map[string]any, ops []PatchDashboardOp) ([]patchChange, error) {
	changes := make([]patchChange, 0, len(ops))
	for i, op := range ops {
		change, err := applyPatchOp(doc, op)
		if err != nil {
			return nil, opError(i, op, err)
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func applyPatchOp(doc map[string]any, op PatchDashboardOp) (patchChange, error) {
	panels, _ := doc["panels"].([]any)
	change := patchChange{Op: op.Op, PanelID: op.PanelID}
	switch op.Op {
	case "add_panel":
		id, _ := op.Panel["id"].(string)
		if id != "" && findPanel(panels, id) >= 0 {
			return change, fmt.Errorf("a panel with id %q already exists; use update_panel", id)
		}
		at := len(panels)
		if op.Index != nil {
			at = *op.Index
		}
		if at < 0 || at > len(panels) {
			return change, fmt.Errorf("index %d out of range 0..%d", at, len(panels))
		}
		doc["panels"] = append(panels[:at], append([]any{op.Panel}, panels[at:]...)...)
		change.PanelID, change.Index = id, &at
	case "update_panel", "remove_panel", "move_panel":
		at := findPanel(panels, op.PanelID)
		if at < 0 {
			return change, fmt.Errorf("no panel with id %q; get_dashboard lists panel ids", op.PanelID)
		}
		panel, _ := panels[at].(map[string]any)
		switch op.Op {
		case "update_panel":
			change.Fields = updatePanelFields(panel, op.Fields)
		case "remove_panel":
			change.Name, _ = panel["name"].(string)
			doc["panels"] = append(panels[:at:at], panels[at+1:]...)
		case "move_panel":
			to := *op.Index
			if to < 0 || to >= len(panels) {
				return change, fmt.Errorf("index %d out of range 0..%d", to, len(panels)-1)
			}
			rest := append(panels[:at:at], panels[at+1:]...)
			doc["panels"] = append(rest[:to:to], append([]any{panel}, rest[to:]...)...)
			change.From, change.To = &at, &to
		}
	case "set_title":
		change.Before, change.After = diffValue(doc["name"]), diffValue(op.Title)
		doc["name"] = op.Title
	case "set_variable":
		change.Target, change.Action = setVariable(doc, op.Variable)
	}
	return change, nil
}

func findPanel(panels []any, id string) int {
	for i, raw := range panels {
		if panel, ok := raw.(map[string]any); ok && panel["id"] == id {
			return i
		}
	}
	return -1
}

func updatePanelFields(panel map[string]any, fields map[string]any) map[string]fieldChange {
	changed := map[string]fieldChange{}
	for key, value := range fields {
		before, had := panel[key]
		if had && reflect.DeepEqual(before, value) {
			continue
		}
		fc := fieldChange{}
		if had {
			fc.Before = diffValue(before)
		}
		if value == nil {
			delete(panel, key)
		} else {
			panel[key] = value
			fc.After = diffValue(value)
		}
		changed[key] = fc
	}
	return changed
}

func setVariable(doc map[string]any, variable map[string]any) (target, action string) {
	target, _ = variable["target"].(string)
	variables, _ := doc["variables"].([]any)
	for i, raw := range variables {
		if existing, ok := raw.(map[string]any); ok && existing["target"] == target {
			variables[i] = variable
			return target, "updated"
		}
	}
	doc["variables"] = append(variables, variable)
	return target, "added"
}

// diffValue renders v as compact JSON, or as a truncated JSON string when it is long,
// so one changed query cannot blow the diff past maxPatchDiffChars.
func diffValue(v any) json.RawMessage {
	raw := marshalNoEscape(v)
	if len(raw) <= maxDiffValueChars {
		return raw
	}
	cut := maxDiffValueChars
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	return marshalNoEscape(string(raw[:cut]) + "…")
}

func renderPatchDiff(diff patchDiff) string {
	text := string(marshalNoEscape(diff))
	if len(text) <= maxPatchDiffChars {
		return text
	}
	for i := range diff.Changes {
		diff.Changes[i].Before, diff.Changes[i].After = nil, nil
		for key := range diff.Changes[i].Fields {
			diff.Changes[i].Fields[key] = fieldChange{}
		}
	}
	diff.Note = "before/after values omitted to keep the diff short; get_dashboard shows the result"
	return string(marshalNoEscape(diff))
}

func marshalNoEscape(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSpace(buf.Bytes())
}
