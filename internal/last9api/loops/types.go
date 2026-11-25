// Package loops calls org-scoped Loops routes on the Last9 API.
package loops

import "encoding/json"

// Status is the agent-facing enablement probe for Loops.
type Status struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// Template summarizes a create-from-template option.
type Template struct {
	Slug           string   `json:"slug"`
	Name           string   `json:"name,omitempty"`
	Description    string   `json:"description,omitempty"`
	RequiredParams []string `json:"required_params,omitempty"`
	Connectors     []string `json:"connectors,omitempty"`
	Capability     string   `json:"capability,omitempty"`
}

// LastRun is a compact run pointer on a loop summary.
type LastRun struct {
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
}

// Summary is the list/get projection agents need before create or lifecycle tools.
type Summary struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status,omitempty"`
	Enabled    bool     `json:"enabled"`
	ActionMode string   `json:"action_mode,omitempty"`
	LastRun    *LastRun `json:"last_run,omitempty"`
}

// Detail is get_loop output: summary plus graph gaps, never secret material.
type Detail struct {
	Summary
	SetupGaps []string        `json:"setup_gaps,omitempty"`
	Graph     json.RawMessage `json:"graph_summary,omitempty"`
}
