package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// writeTools is every tool that changes Last9 state. A new write tool must be
// added here on purpose; any tool not listed must be served as read-only.
var writeTools = map[string]struct{ destructive, idempotent bool }{
	"add_drop_rule":             {destructive: true, idempotent: false},
	"create_dashboard":          {destructive: false, idempotent: false},
	"update_dashboard":          {destructive: true, idempotent: true},
	"delete_dashboard":          {destructive: true, idempotent: true},
	"delete_dashboard_snapshot": {destructive: true, idempotent: true},
}

func TestDumpToolsServesAnnotations(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpTools(&buf, nil); err != nil {
		t.Fatalf("dumpTools failed: %v", err)
	}
	var out struct {
		Tools []struct {
			Name        string `json:"name"`
			Title       string `json:"title"`
			Annotations *struct {
				Title           string `json:"title"`
				ReadOnlyHint    bool   `json:"readOnlyHint"`
				DestructiveHint *bool  `json:"destructiveHint"`
				IdempotentHint  bool   `json:"idempotentHint"`
				OpenWorldHint   *bool  `json:"openWorldHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	served := make(map[string]bool, len(out.Tools))
	for _, tool := range out.Tools {
		served[tool.Name] = true
		a := tool.Annotations
		if a == nil {
			t.Errorf("%s: served without annotations", tool.Name)
			continue
		}
		if a.Title == "" || tool.Title != a.Title {
			t.Errorf("%s: title %q and annotations.title %q must both be set and equal", tool.Name, tool.Title, a.Title)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s: openWorldHint must be served as false", tool.Name)
		}
		want, isWrite := writeTools[tool.Name]
		if !isWrite {
			if !a.ReadOnlyHint {
				t.Errorf("%s: read tool must be served with readOnlyHint: true", tool.Name)
			}
			continue
		}
		if a.ReadOnlyHint {
			t.Errorf("%s: write tool must not be served as read-only", tool.Name)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint != want.destructive {
			t.Errorf("%s: destructiveHint must be served as %v", tool.Name, want.destructive)
		}
		if a.IdempotentHint != want.idempotent {
			t.Errorf("%s: idempotentHint = %v, want %v", tool.Name, a.IdempotentHint, want.idempotent)
		}
	}
	for name := range writeTools {
		if !served[name] {
			t.Errorf("write tool %s is not served; update writeTools", name)
		}
	}
}
