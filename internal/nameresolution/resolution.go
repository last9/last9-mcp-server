package nameresolution

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/namematch"
)

// Catalog is usable after a scoped listing succeeds without known partial coverage.
// It is not proof that the telemetry inventory is exhaustive.
type Catalog struct {
	Names  []string
	Usable bool
}

type Resolution struct {
	Entity     string   `json:"entity"`
	Requested  string   `json:"requested"`
	Status     string   `json:"status"`
	Canonical  string   `json:"canonical,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Omitted    int      `json:"omitted,omitempty"`
}

func Resolve(entity, requested string, catalog Catalog) Resolution {
	names := slices.Clone(catalog.Names)
	slices.Sort(names)
	names = slices.Compact(names)
	if !catalog.Usable {
		r := Resolution{Entity: entity, Requested: requested, Status: "incomplete"}
		if slices.Contains(names, requested) {
			r.Status = "exact"
			r.Canonical = requested
			return r
		}
		r.Candidates = names[:min(len(names), namematch.MaxCandidates)]
		r.Omitted = max(0, len(names)-namematch.MaxCandidates)
		return r
	}
	match := namematch.Resolve(requested, names)
	r := Resolution{Entity: entity, Requested: requested, Candidates: match.Candidates, Omitted: match.Omitted}
	switch {
	case match.Kind == namematch.Exact:
		r.Status, r.Canonical = "exact", match.Name
	case match.Kind == namematch.Closest:
		r.Status, r.Canonical = "closest", match.Name
	case match.Kind == namematch.Ambiguous:
		r.Status = "ambiguous"
	default:
		r.Status = "missing"
	}
	return r
}

func (r Resolution) Note() string {
	if r.Status != "closest" {
		return ""
	}
	return fmt.Sprintf("Using %s %q (closest match to %q).", r.Entity, r.Canonical, r.Requested)
}

func (r Resolution) Result() *mcp.CallToolResult {
	if r.Status == "exact" || r.Status == "closest" {
		return nil
	}
	message := "No measurements returned. Retry with an exact candidate in the same scope."
	if r.Status == "missing" {
		message = "No matching name in this discovery scope. Check the name or explicitly change the scope."
	}
	if r.Status == "incomplete" {
		message = "Discovery was incomplete; uniqueness and absence are unknown. Retry with an exact name or narrow the scope."
	}
	payload, _ := json.Marshal(struct {
		Resolution
		Message string `json:"message"`
	}{r, message})
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}
}
