package toolsets

import (
	"reflect"
	"testing"
)

func TestFilterAllows(t *testing.T) {
	alerts, err := Parse("alerts")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		filter   Filter
		tool     string
		readOnly bool
		want     bool
	}{
		{name: "zero filter serves a write tool", tool: "create_alert", want: true},
		{name: "zero filter serves a read tool", tool: "get_logs", readOnly: true, want: true},
		{name: "read-only skips a write tool", filter: Filter{ReadOnly: true}, tool: "create_alert", want: false},
		{name: "read-only serves a read tool", filter: Filter{ReadOnly: true}, tool: "get_logs", readOnly: true, want: true},
		{name: "read-only serves an allowed write tool", filter: Filter{ReadOnly: true, AllowWrite: Set{"create_alert": {}}}, tool: "create_alert", want: true},
		{name: "allow list does not leak to other write tools", filter: Filter{ReadOnly: true, AllowWrite: Set{"create_alert": {}}}, tool: "delete_alert", want: false},
		{name: "toolset excludes a read tool", filter: Filter{Tools: alerts}, tool: "get_logs", readOnly: true, want: false},
		{name: "toolset excludes an allowed write tool outside it", filter: Filter{Tools: alerts, ReadOnly: true, AllowWrite: Set{"create_dashboard": {}}}, tool: "create_dashboard", want: false},
		{name: "toolset and allow list together serve an alerts write tool", filter: Filter{Tools: alerts, ReadOnly: true, AllowWrite: Set{"create_alert": {}}}, tool: "create_alert", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Allows(tt.tool, tt.readOnly); got != tt.want {
				t.Fatalf("Allows(%q, readOnly=%v) = %v, want %v", tt.tool, tt.readOnly, got, tt.want)
			}
		})
	}
}

func TestFilterServesWrite(t *testing.T) {
	allowCreate := Filter{ReadOnly: true, AllowWrite: Set{"create_alert": {}}}
	if !(Filter{}).ServesWrite("create_alert") || !allowCreate.ServesWrite("create_alert") {
		t.Fatal("ServesWrite(create_alert) = false where the write tool is served")
	}
	if (Filter{ReadOnly: true}).ServesWrite("create_alert") || allowCreate.ServesWrite("patch_alert") {
		t.Fatal("ServesWrite = true for a write tool read-only mode hides")
	}
}

func TestNewFilter(t *testing.T) {
	alerts, err := Parse("alerts")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		toolsets   string
		readOnly   bool
		allowWrite string
		want       Filter
		wantErr    bool
	}{
		{name: "all defaults", want: Filter{}},
		{name: "read-only alone", readOnly: true, want: Filter{ReadOnly: true}},
		{name: "allow list trims and drops empties", readOnly: true, allowWrite: " create_alert, ,patch_alert,", want: Filter{ReadOnly: true, AllowWrite: Set{"create_alert": {}, "patch_alert": {}}}},
		{name: "empty allow list with read-only", readOnly: true, allowWrite: " , ", want: Filter{ReadOnly: true}},
		{name: "empty allow list without read-only", allowWrite: " ", want: Filter{}},
		{name: "allow list without read-only errors", allowWrite: "create_alert", wantErr: true},
		{name: "toolsets combine with read-only", toolsets: "alerts", readOnly: true, want: Filter{Tools: alerts, ReadOnly: true}},
		{name: "unknown toolset errors", toolsets: "bogus", readOnly: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFilter(tt.toolsets, tt.readOnly, tt.allowWrite)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewFilter = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewFilter: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NewFilter = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadOnlyFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    bool
		wantErr bool
	}{
		{name: "unset is off", want: false},
		{name: "true", env: map[string]string{"LAST9_MCP_READ_ONLY": "true"}, want: true},
		{name: "1", env: map[string]string{"LAST9_MCP_READ_ONLY": "1"}, want: true},
		{name: "false", env: map[string]string{"LAST9_MCP_READ_ONLY": "false"}, want: false},
		{name: "empty is off", env: map[string]string{"LAST9_MCP_READ_ONLY": ""}, want: false},
		{name: "garbage errors instead of reading as false", env: map[string]string{"LAST9_MCP_READ_ONLY": "yes please"}, wantErr: true},
		{name: "alias", env: map[string]string{"LAST9_READ_ONLY": "true"}, want: true},
		{name: "alias wins like the ff flag env", env: map[string]string{"LAST9_READ_ONLY": "false", "LAST9_MCP_READ_ONLY": "true"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LAST9_MCP_READ_ONLY", "")
			t.Setenv("LAST9_READ_ONLY", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, err := ReadOnlyFromEnv()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadOnlyFromEnv = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadOnlyFromEnv: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ReadOnlyFromEnv = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllowWriteSpecFromEnv(t *testing.T) {
	t.Setenv("LAST9_ALLOW_WRITE_TOOLS", "")
	t.Setenv("LAST9_MCP_ALLOW_WRITE_TOOLS", "create_alert")
	if got := AllowWriteSpecFromEnv(); got != "create_alert" {
		t.Fatalf("AllowWriteSpecFromEnv = %q, want create_alert", got)
	}
	t.Setenv("LAST9_ALLOW_WRITE_TOOLS", "patch_alert")
	if got := AllowWriteSpecFromEnv(); got != "patch_alert" {
		t.Fatalf("AllowWriteSpecFromEnv = %q, want the LAST9_ALLOW_WRITE_TOOLS alias", got)
	}
}
