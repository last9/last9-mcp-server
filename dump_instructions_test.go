package main

import (
	"bytes"
	"strings"
	"testing"

	"last9-mcp/internal/toolsets"
)

func TestDumpInstructionsPrintsPolicyForToolsets(t *testing.T) {
	tests := []struct {
		spec      string
		mentions  []string
		omissions []string
	}{
		{spec: "", mentions: []string{"`get_logs`", "`create_dashboard`"}},
		{spec: "logs", mentions: []string{"`get_logs`"}, omissions: []string{"`create_dashboard`"}},
		{spec: "dashboards", mentions: []string{"`create_dashboard`"}, omissions: []string{"`get_logs`"}},
	}
	for _, tt := range tests {
		t.Run("toolsets="+tt.spec, func(t *testing.T) {
			allowed, err := toolsets.Parse(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := dumpInstructions(&buf, allowed); err != nil {
				t.Fatalf("dumpInstructions error: %v", err)
			}
			want, err := serverInstructions(allowed)
			if err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != want+"\n" {
				t.Fatalf("dump-instructions = %q, want the served policy %q", got, want+"\n")
			}
			for _, s := range tt.mentions {
				if !strings.Contains(buf.String(), s) {
					t.Errorf("dump-instructions missing %q", s)
				}
			}
			for _, s := range tt.omissions {
				if strings.Contains(buf.String(), s) {
					t.Errorf("dump-instructions contains %q", s)
				}
			}
		})
	}
}
