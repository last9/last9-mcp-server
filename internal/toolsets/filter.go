package toolsets

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Filter decides which tools a server serves. A tool must pass the toolset
// allow-list and, when ReadOnly is on, be read-only or named in AllowWrite.
// The zero value serves every tool.
type Filter struct {
	Tools      Set // nil allows every toolset
	ReadOnly   bool
	AllowWrite Set // write tools still served when ReadOnly is on
}

// NewFilter builds a Filter from the --toolsets, --read-only and
// --allow-write-tools values. An allow list without read-only is an error:
// every write tool is already served, so the list would silently do nothing.
func NewFilter(toolsetSpec string, readOnly bool, allowWriteSpec string) (Filter, error) {
	tools, err := Parse(toolsetSpec)
	if err != nil {
		return Filter{}, err
	}
	var allowWrite Set
	for _, name := range strings.Split(allowWriteSpec, ",") {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if allowWrite == nil {
			allowWrite = make(Set)
		}
		allowWrite[name] = struct{}{}
	}
	if allowWrite != nil && !readOnly {
		return Filter{}, errors.New("--allow-write-tools requires --read-only: without it every write tool is already served")
	}
	return Filter{Tools: tools, ReadOnly: readOnly, AllowWrite: allowWrite}, nil
}

// Allows reports whether a tool may be registered. readOnly is the tool's own
// readOnlyHint; callers pass false for a tool without annotations.
func (f Filter) Allows(toolName string, readOnly bool) bool {
	if !f.Tools.Allows(toolName) {
		return false
	}
	if !f.ReadOnly || readOnly {
		return true
	}
	_, ok := f.AllowWrite[toolName]
	return ok
}

// ReadOnlyFromEnv returns the read-only default from the environment.
// LAST9_READ_ONLY (the ff flag env) wins over LAST9_MCP_READ_ONLY, as for
// toolsets. A value that is not a boolean is an error, never a silent false.
func ReadOnlyFromEnv() (bool, error) {
	for _, key := range []string{"LAST9_READ_ONLY", "LAST9_MCP_READ_ONLY"} {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		readOnly, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("%s=%q is not a boolean; use true or false", key, v)
		}
		return readOnly, nil
	}
	return false, nil
}

// AllowWriteSpecFromEnv returns the --allow-write-tools default from the
// environment. Prefer LAST9_ALLOW_WRITE_TOOLS; fall back to LAST9_MCP_ALLOW_WRITE_TOOLS.
func AllowWriteSpecFromEnv() string {
	if v := os.Getenv("LAST9_ALLOW_WRITE_TOOLS"); v != "" {
		return v
	}
	return os.Getenv("LAST9_MCP_ALLOW_WRITE_TOOLS")
}
