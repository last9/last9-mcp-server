package logs

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAddServiceLogsEnvFilter_UsesResourceAttribute verifies the env filter uses
// resources['deployment.environment'] — consistent with get_service_traces and
// apm/databases. The wrong key attributes['deployment_environment'] silently
// matches nothing, returning all-env logs while the dashboard scopes to one env.
func TestAddServiceLogsEnvFilter_UsesResourceAttribute(t *testing.T) {
	base := buildServiceLogsQuery("api", nil, nil)
	result := addServiceLogsEnvFilter(base, "production")

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	s := string(raw)

	if !strings.Contains(s, `resources['deployment.environment']`) {
		t.Errorf("env filter must use resources['deployment.environment'], got: %s", s)
	}

	if strings.Contains(s, `attributes['deployment_environment']`) {
		t.Errorf("env filter must not use attributes['deployment_environment'] (wrong namespace + underscore key), got: %s", s)
	}
}

// TestAddServiceLogsEnvFilter_ValueIsPreserved ensures the env value is present in
// the generated filter condition.
func TestAddServiceLogsEnvFilter_ValueIsPreserved(t *testing.T) {
	base := buildServiceLogsQuery("svc", nil, nil)
	result := addServiceLogsEnvFilter(base, "staging")

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(raw), "staging") {
		t.Errorf("env value 'staging' missing from filter: %s", string(raw))
	}
}

// TestAddServiceLogsEnvFilter_EmptyEnvIsNoop verifies that passing empty env leaves
// query unchanged — no phantom env condition is injected.
func TestAddServiceLogsEnvFilter_EmptyEnvIsNoop(t *testing.T) {
	base := buildServiceLogsQuery("api", nil, nil)
	before, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	result := addServiceLogsEnvFilter(base, "")
	after, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if string(before) != string(after) {
		t.Errorf("empty env must not modify query\nbefore: %s\nafter:  %s", before, after)
	}
}
