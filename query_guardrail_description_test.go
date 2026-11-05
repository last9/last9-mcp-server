package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"last9-mcp/internal/toolsets"
)

func TestServedQueryGuardrailDescriptions(t *testing.T) {
	for _, domain := range []string{"all", "logs", "traces", "investigate"} {
		t.Run(domain, func(t *testing.T) {
			allowed, err := toolsets.Parse(domain)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := dumpTools(&buf, toolsets.Filter{Tools: allowed}); err != nil {
				t.Fatal(err)
			}
			var out struct {
				Tools []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"get_logs", "get_service_logs", "get_traces", "get_service_traces"} {
				if domain == "logs" && strings.Contains(name, "traces") || domain == "traces" && strings.Contains(name, "logs") {
					continue
				}
				t.Run(name, func(t *testing.T) {
					var description string
					for _, tool := range out.Tools {
						if tool.Name == name {
							description = tool.Description
						}
					}
					if description == "" {
						t.Fatal("guarded tool missing from tools/list")
					}
					for _, want := range []string{"Guardrail", "server-configured", "`off` (default)", "`shadow`", "`enforce`", "reject", "reason-coded `hints`", "narrow the window", "estimate_unavailable"} {
						if !strings.Contains(description, want) {
							t.Errorf("served description missing %q", want)
						}
					}
					if name == "get_service_traces" && !strings.Contains(description, "Exact `trace_id` lookups bypass the guardrail") {
						t.Error("served description must state the exact-ID exemption")
					}
				})
			}
		})
	}
}
