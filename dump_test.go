package main

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func TestDumpTools(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpTools(&buf); err != nil {
		t.Fatalf("dumpTools failed: %v", err)
	}

	var out struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// All registered tools must be covered — the whole point of dump-tools.
	// A loose floor would let a regression silently drop tools. Tighten this
	// when the committed snapshot + CI equality gate supersedes it.
	if len(out.Tools) < 37 {
		t.Fatalf("expected at least 37 tools, got %d", len(out.Tools))
	}
	if !sort.SliceIsSorted(out.Tools, func(i, j int) bool { return out.Tools[i].Name < out.Tools[j].Name }) {
		t.Fatal("tools are not sorted by name (output must be deterministic for snapshot diffing)")
	}

	byName := make(map[string]int)
	for i, tool := range out.Tools {
		byName[tool.Name] = i
	}
	for _, name := range []string{"get_traces", "get_service_summary", "prometheus_label_values", "get_logs"} {
		i, ok := byName[name]
		if !ok {
			t.Fatalf("tool %q missing from dump", name)
		}
		if strings.TrimSpace(out.Tools[i].Description) == "" {
			t.Fatalf("tool %q has empty description", name)
		}
		if out.Tools[i].InputSchema == nil {
			t.Fatalf("tool %q has no inputSchema", name)
		}
	}

	// The {{labels}} placeholder must never leak into served descriptions —
	// enhancement substitutes it (empty on a cold cache).
	if strings.Contains(out.Tools[byName["get_logs"]].Description, "{{labels}}") {
		t.Fatal("get_logs description still contains unsubstituted {{labels}} placeholder")
	}
}
