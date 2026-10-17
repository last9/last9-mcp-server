package main

import (
	"os"
	"reflect"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
)

func clearFilterEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"LAST9_TOOLSETS", "LAST9_MCP_TOOLSETS",
		"LAST9_READ_ONLY", "LAST9_MCP_READ_ONLY",
		"LAST9_ALLOW_WRITE_TOOLS", "LAST9_MCP_ALLOW_WRITE_TOOLS",
	} {
		t.Setenv(key, "")
	}
}

type filterFlagCase struct {
	name    string
	env     map[string]string
	args    []string
	want    toolsets.Filter
	wantErr bool
}

func filterFlagCases(t *testing.T) []filterFlagCase {
	t.Helper()
	alerts, err := toolsets.Parse("alerts")
	if err != nil {
		t.Fatal(err)
	}
	return []filterFlagCase{
		{name: "defaults serve everything", want: toolsets.Filter{}},
		{name: "env true", env: map[string]string{"LAST9_MCP_READ_ONLY": "true"}, want: toolsets.Filter{ReadOnly: true}},
		{name: "env 1", env: map[string]string{"LAST9_MCP_READ_ONLY": "1"}, want: toolsets.Filter{ReadOnly: true}},
		{name: "env false", env: map[string]string{"LAST9_MCP_READ_ONLY": "false"}, want: toolsets.Filter{}},
		{name: "env garbage is a startup error", env: map[string]string{"LAST9_MCP_READ_ONLY": "maybe"}, wantErr: true},
		{name: "alias env true", env: map[string]string{"LAST9_READ_ONLY": "true"}, want: toolsets.Filter{ReadOnly: true}},
		{name: "alias env true wins over prefixed false", env: map[string]string{"LAST9_READ_ONLY": "true", "LAST9_MCP_READ_ONLY": "false"}, want: toolsets.Filter{ReadOnly: true}},
		{name: "alias env false wins over prefixed true", env: map[string]string{"LAST9_READ_ONLY": "false", "LAST9_MCP_READ_ONLY": "true"}, want: toolsets.Filter{}},
		{name: "flag", args: []string{"--read-only"}, want: toolsets.Filter{ReadOnly: true}},
		{name: "flag false overrides env true", env: map[string]string{"LAST9_MCP_READ_ONLY": "true"}, args: []string{"--read-only=false"}, want: toolsets.Filter{}},
		{name: "env allow list", env: map[string]string{"LAST9_MCP_READ_ONLY": "true", "LAST9_MCP_ALLOW_WRITE_TOOLS": "create_alert,patch_alert"}, want: toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}, "patch_alert": {}}}},
		{name: "alias env allow list", env: map[string]string{"LAST9_READ_ONLY": "true", "LAST9_ALLOW_WRITE_TOOLS": "create_alert"}, want: toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}}}},
		{name: "alias env allow list wins over prefixed", env: map[string]string{"LAST9_READ_ONLY": "true", "LAST9_ALLOW_WRITE_TOOLS": "create_alert", "LAST9_MCP_ALLOW_WRITE_TOOLS": "patch_alert"}, want: toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}}}},
		{name: "empty env allow list", env: map[string]string{"LAST9_MCP_READ_ONLY": "true", "LAST9_MCP_ALLOW_WRITE_TOOLS": " "}, want: toolsets.Filter{ReadOnly: true}},
		{name: "allow list without read-only is a startup error", args: []string{"--allow-write-tools=create_alert"}, wantErr: true},
		{name: "all three flags", args: []string{"--toolsets=alerts", "--read-only", "--allow-write-tools=create_alert"}, want: toolsets.Filter{Tools: alerts, ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}}}},
	}
}

func setFilterEnv(t *testing.T, env map[string]string) {
	t.Helper()
	clearFilterEnv(t)
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestParseDumpToolFilter(t *testing.T) {
	for _, tt := range filterFlagCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			setFilterEnv(t, tt.env)
			got, err := parseDumpToolFilter(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDumpToolFilter(%v) = %+v, want error", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDumpToolFilter(%v): %v", tt.args, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseDumpToolFilter(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestSetupConfigToolFilter(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	for _, tt := range filterFlagCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			setFilterEnv(t, tt.env)
			os.Args = append([]string{"last9-mcp"}, tt.args...)
			cfg, err := SetupConfig(models.Config{RefreshToken: "test-token"})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SetupConfig(%v) filter = %+v, want error", tt.args, cfg.ToolFilter)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetupConfig(%v): %v", tt.args, err)
			}
			if !reflect.DeepEqual(cfg.ToolFilter, tt.want) {
				t.Fatalf("SetupConfig(%v) filter = %+v, want %+v", tt.args, cfg.ToolFilter, tt.want)
			}
		})
	}
}
