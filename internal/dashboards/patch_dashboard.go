package dashboards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apidashboards "last9-mcp/internal/last9api/dashboards"
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
	// NotStored lists panel keys that were sent but are absent from the stored panel;
	// NotRemoved lists keys removed with null that the stored panel still has.
	NotStored  []string `json:"not_stored,omitempty"`
	NotRemoved []string `json:"not_removed,omitempty"`

	added map[string]any
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
		if _, err := resolveRegion(cfg, ""); err != nil {
			return nil, nil, err
		}

		api := last9api.NewClient(client, cfg)
		current, err := apidashboards.GetDashboard(ctx, api, args.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("%w; nothing was written", mapDashboardAPIError(err))
		}
		if current.Dashboard == nil {
			return nil, nil, errors.New("get dashboard returned no dashboard object; nothing was written")
		}

		changes, err := applyPatchOps(current.Dashboard, args.Ops)
		if err != nil {
			return nil, nil, err
		}
		if len(changes) == 0 {
			return patchResult(dlBuilder, patchDiff{DashboardID: args.ID, Changes: changes,
				Note: "the dashboard already matches every op; nothing was written"}), nil, nil
		}

		dashboard, err := json.Marshal(current.Dashboard)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal dashboard: %w", err)
		}
		payload, err := marshalDashboardRequest(DashboardRequest{Dashboard: dashboard, Metadata: current.Metadata})
		if err != nil {
			return nil, nil, err
		}
		if err := apidashboards.UpdateDashboard(ctx, api, args.ID, payload); err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		diff := patchDiff{DashboardID: args.ID, Changes: changes}
		// The API silently drops panel keys it does not know, so the diff is checked
		// against a re-read rather than trusted as sent.
		stored, err := apidashboards.GetDashboard(ctx, api, args.ID)
		if err != nil || stored.Dashboard == nil {
			diff.Note = "written, but re-reading the dashboard failed, so changes show what was sent; get_dashboard shows what was stored"
		} else {
			diff.Note = reconcileStored(changes, current.Dashboard, stored.Dashboard)
		}
		return patchResult(dlBuilder, diff), nil, nil
	}
}

func patchResult(dlBuilder *deeplink.Builder, diff patchDiff) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: renderPatchDiff(diff)}},
		Meta:    deeplink.ToMeta(dlBuilder.BuildDashboardLink(diff.DashboardID)),
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
		change, changed, err := applyPatchOp(doc, op)
		if err != nil {
			return nil, opError(i, op, err)
		}
		if changed {
			changes = append(changes, change)
		}
	}
	return changes, nil
}

// applyPatchOp reports changed=false for an op the dashboard already satisfies, so it
// neither shows up in the diff nor causes a write.
func applyPatchOp(doc map[string]any, op PatchDashboardOp) (change patchChange, changed bool, err error) {
	panels, _ := doc["panels"].([]any)
	change = patchChange{Op: op.Op, PanelID: op.PanelID}
	switch op.Op {
	case "add_panel":
		id, _ := op.Panel["id"].(string)
		if id != "" && findPanel(panels, id) >= 0 {
			return change, false, fmt.Errorf("a panel with id %q already exists; use update_panel", id)
		}
		at := len(panels)
		if op.Index != nil {
			at = *op.Index
		}
		if at < 0 || at > len(panels) {
			return change, false, fmt.Errorf("index %d out of range 0..%d", at, len(panels))
		}
		doc["panels"] = append(panels[:at], append([]any{op.Panel}, panels[at:]...)...)
		change.PanelID, change.Index, change.added = id, &at, op.Panel
	case "update_panel", "remove_panel", "move_panel":
		at := findPanel(panels, op.PanelID)
		if at < 0 {
			return change, false, fmt.Errorf("no panel with id %q; get_dashboard lists panel ids", op.PanelID)
		}
		panel, _ := panels[at].(map[string]any)
		switch op.Op {
		case "update_panel":
			change.Fields = updatePanelFields(panel, op.Fields)
			return change, len(change.Fields) > 0, nil
		case "remove_panel":
			name, _ := panel["name"].(string)
			change.Name = truncateDiffText(name)
			doc["panels"] = append(panels[:at:at], panels[at+1:]...)
		case "move_panel":
			to := *op.Index
			if to < 0 || to >= len(panels) {
				return change, false, fmt.Errorf("index %d out of range 0..%d", to, len(panels)-1)
			}
			rest := append(panels[:at:at], panels[at+1:]...)
			doc["panels"] = append(rest[:to:to], append([]any{panel}, rest[to:]...)...)
			change.From, change.To = &at, &to
			return change, at != to, nil
		}
	case "set_title":
		before := doc["name"]
		change.Before, change.After = diffValue(before), diffValue(op.Title)
		doc["name"] = op.Title
		return change, before != op.Title, nil
	case "set_variable":
		change.Target, change.Action = setVariable(doc, op.Variable)
		return change, change.Action != "unchanged", nil
	}
	return change, true, nil
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
		if !had && value == nil || had && value != nil && reflect.DeepEqual(before, value) {
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

func reconcileStored(changes []patchChange, sent, stored map[string]any) (note string) {
	var notStored, notRemoved bool
	for i := range changes {
		c := &changes[i]
		sentPanel := panelByID(sent, c.PanelID)
		if c.Op == "add_panel" {
			sentPanel = c.added
			if c.PanelID == "" {
				c.PanelID = assignedPanelID(sent, stored, c.added)
			}
		}
		if c.PanelID == "" || sentPanel == nil || (c.Op != "update_panel" && c.Op != "add_panel") {
			continue
		}
		storedPanel := panelByID(stored, c.PanelID)
		keys := slices.Sorted(maps.Keys(c.Fields))
		if c.Op == "add_panel" {
			keys = slices.Sorted(maps.Keys(sentPanel))
		}
		for _, key := range keys {
			sentValue, sentHas := sentPanel[key]
			storedValue, storedHas := storedPanel[key]
			if sentHas && !storedHas {
				c.NotStored = append(c.NotStored, key)
				delete(c.Fields, key)
			} else if !sentHas && storedHas {
				c.NotRemoved = append(c.NotRemoved, key)
				delete(c.Fields, key)
			} else if fc, ok := c.Fields[key]; ok && storedHas && !reflect.DeepEqual(sentValue, storedValue) {
				fc.After = diffValue(storedValue)
				c.Fields[key] = fc
			}
		}
		notStored = notStored || len(c.NotStored) > 0
		notRemoved = notRemoved || len(c.NotRemoved) > 0
	}
	switch {
	case notStored && notRemoved:
		return "not_stored lists keys the dashboard API did not keep, not_removed keys it kept or restored to a default; get_dashboard shows the stored panel"
	case notStored:
		return "not_stored lists keys the dashboard API did not keep; get_dashboard shows the stored panel"
	case notRemoved:
		return "not_removed lists keys the dashboard API kept or restored to a default; get_dashboard shows the stored panel"
	}
	return ""
}

// assignedPanelID returns the id the API gave a panel sent without one. Panels are matched
// by position, so it gives up unless the stored panel at that position is a new one.
func assignedPanelID(sent, stored, panel map[string]any) string {
	sentPanels, _ := sent["panels"].([]any)
	storedPanels, _ := stored["panels"].([]any)
	if len(sentPanels) != len(storedPanels) {
		return ""
	}
	for i, p := range sentPanels {
		if m, ok := p.(map[string]any); !ok || reflect.ValueOf(m).UnsafePointer() != reflect.ValueOf(panel).UnsafePointer() {
			continue
		}
		storedPanel, _ := storedPanels[i].(map[string]any)
		if id, _ := storedPanel["id"].(string); id != "" && panelByID(sent, id) == nil {
			return id
		}
		return ""
	}
	return ""
}

func panelByID(doc map[string]any, id string) map[string]any {
	panels, _ := doc["panels"].([]any)
	if at := findPanel(panels, id); at >= 0 {
		panel, _ := panels[at].(map[string]any)
		return panel
	}
	return nil
}

func setVariable(doc map[string]any, variable map[string]any) (target, action string) {
	target, _ = variable["target"].(string)
	variables, _ := doc["variables"].([]any)
	for i, raw := range variables {
		if existing, ok := raw.(map[string]any); ok && existing["target"] == target {
			if reflect.DeepEqual(existing, variable) {
				return target, "unchanged"
			}
			variables[i] = variable
			return target, "updated"
		}
	}
	doc["variables"] = append(variables, variable)
	return target, "added"
}

// diffValue renders v as compact JSON, shortened in place when it is long so one changed
// query cannot blow the diff past maxPatchDiffChars while the value keeps its JSON type.
func diffValue(v any) json.RawMessage {
	raw := marshalNoEscape(v)
	if len(raw) <= maxDiffValueChars {
		return raw
	}
	return marshalNoEscape(shrinkValue(v, maxDiffValueChars))
}

func shrinkValue(v any, budget int) any {
	child := budget / 4
	switch v := v.(type) {
	case string:
		return truncateText(v, budget)
	case []any:
		out := []any{}
		for i, elem := range v {
			if budget <= 0 {
				return append(out, fmt.Sprintf("… %d more", len(v)-i))
			}
			short := shrinkValue(elem, min(budget, child))
			out = append(out, short)
			budget -= len(marshalNoEscape(short)) + 1
		}
		return out
	case map[string]any:
		out := map[string]any{}
		keys := slices.Sorted(maps.Keys(v))
		for i, key := range keys {
			if budget <= 0 {
				out["…"] = fmt.Sprintf("%d more keys", len(keys)-i)
				break
			}
			short := shrinkValue(v[key], min(budget, child))
			out[key] = short
			budget -= len(key) + len(marshalNoEscape(short)) + 4
		}
		return out
	}
	return v
}

func truncateDiffText(s string) string {
	return truncateText(s, maxDiffValueChars)
}

func truncateText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := max(limit, 0)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
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
	const omitted = "before/after values omitted to keep the diff short; get_dashboard shows the result"
	if diff.Note == "" {
		diff.Note = omitted
	} else {
		diff.Note += "; " + omitted
	}
	return string(marshalNoEscape(diff))
}

func marshalNoEscape(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSpace(buf.Bytes())
}
