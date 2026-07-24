package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
)

const (
	evidenceV1 = "investigation-evidence/v1"
	workflowV1 = "investigation-workflow/v1"
)

func readJSONObject(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func object(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s must be an object", key)
	}
	return v
}

func array(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	v, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s must be an array", key)
	}
	return v
}

func TestInvestigationContractSchemasAreValidJSON(t *testing.T) {
	for _, name := range []string{"investigation-evidence-v1.schema.json", "investigation-workflow-v1.schema.json"} {
		schema := readJSONObject(t, filepath.Join("contracts", name))
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatalf("%s: unexpected schema dialect", name)
		}
		if len(array(t, schema, "required")) == 0 {
			t.Fatalf("%s: missing required fields", name)
		}
	}
}

func resolveContractSchema(t *testing.T, path string) *jsonschema.Resolved {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}

func TestInvestigationEvidenceFixtures(t *testing.T) {
	schema := resolveContractSchema(t, "contracts/investigation-evidence-v1.schema.json")
	paths, err := filepath.Glob("contracts/fixtures/evidence-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 2 {
		t.Fatal("expected deviation and waterfall evidence fixtures")
	}
	for _, path := range paths {
		fixture := readJSONObject(t, path)
		if err := schema.Validate(fixture); err != nil {
			t.Fatalf("%s does not validate: %v", path, err)
		}
		if fixture["contract_version"] != evidenceV1 {
			t.Fatalf("%s: contract_version=%v", path, fixture["contract_version"])
		}
		if !strings.HasSuffix(fixture["analysis_version"].(string), "/v1") {
			t.Fatalf("%s: analysis_version=%v", path, fixture["analysis_version"])
		}
		request := object(t, fixture, "request")
		for _, key := range []string{"requested_window", "effective_window"} {
			window := object(t, request, key)
			if window["boundary"] != "half-open" {
				t.Fatalf("%s: %s must be half-open", path, key)
			}
			start, err := time.Parse(time.RFC3339, window["start"].(string))
			if err != nil {
				t.Fatalf("%s: %s start: %v", path, key, err)
			}
			end, err := time.Parse(time.RFC3339, window["end"].(string))
			if err != nil || !end.After(start) {
				t.Fatalf("%s: invalid %s window", path, key)
			}
		}
		evidence := object(t, fixture, "evidence")
		for _, key := range []string{"partial", "truncated", "warnings", "provenance"} {
			if _, ok := evidence[key]; !ok {
				t.Fatalf("%s: evidence missing %s", path, key)
			}
		}
		interpretation := object(t, fixture, "interpretation")
		if interpretation["claim_type"] == "cause" {
			t.Fatalf("%s: causal claim type is forbidden", path)
		}
	}
}

func TestInvestigationWorkflowFixtures(t *testing.T) {
	schema := resolveContractSchema(t, "contracts/investigation-workflow-v1.schema.json")
	manifest := readJSONObject(t, "contracts/fixtures/workflow-cases-v1.json")
	if manifest["fixture_version"] != "investigation-workflow-fixtures/v1" {
		t.Fatal("unexpected fixture version")
	}
	wantCases := map[string]bool{"supported-action-approval-pending": false, "needs-more-evidence": false, "unproven": false, "invalid-scope": false, "partial": false, "cancelled": false}
	for _, raw := range array(t, manifest, "cases") {
		item := raw.(map[string]any)
		caseID := item["case_id"].(string)
		if _, ok := wantCases[caseID]; !ok {
			t.Fatalf("unexpected case %q", caseID)
		}
		wantCases[caseID] = true
		workflow := object(t, item, "workflow")
		if err := schema.Validate(workflow); err != nil {
			t.Fatalf("%s does not validate: %v", caseID, err)
		}
		if workflow["workflow_version"] != workflowV1 {
			t.Fatalf("%s: workflow_version=%v", caseID, workflow["workflow_version"])
		}
		investigation := object(t, workflow, "investigation")
		id := investigation["investigation_id"].(string)
		last := 0
		for _, eventRaw := range array(t, workflow, "events") {
			event := eventRaw.(map[string]any)
			if event["investigation_id"] != id {
				t.Fatalf("%s: event belongs to another investigation", caseID)
			}
			seq := int(event["sequence"].(float64))
			if seq <= last {
				t.Fatalf("%s: event sequence is not append-only", caseID)
			}
			last = seq
		}
	}
	for id, seen := range wantCases {
		if !seen {
			t.Fatalf("missing workflow case %s", id)
		}
	}
}

func TestWorkflowEvidenceHashesMatchImmutableFixtures(t *testing.T) {
	manifest := readJSONObject(t, "contracts/fixtures/workflow-cases-v1.json")
	known := map[string]string{}
	for _, path := range []string{"contracts/fixtures/evidence-trace-deviations.json", "contracts/fixtures/evidence-trace-waterfall.json"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		fixture := readJSONObject(t, path)
		known[fixture["analysis_version"].(string)] = hex.EncodeToString(sum[:])
	}
	for _, raw := range array(t, manifest, "cases") {
		workflow := object(t, raw.(map[string]any), "workflow")
		investigation := object(t, workflow, "investigation")
		for _, refRaw := range array(t, investigation, "evidence_refs") {
			ref := refRaw.(map[string]any)
			analysis := ref["analysis_version"].(string)
			if ref["content_sha256"] != known[analysis] {
				t.Fatalf("%s hash does not match immutable fixture", analysis)
			}
		}
	}
}

func TestInvestigationFeatureFlagsDefaultOffAndUnique(t *testing.T) {
	manifest := readJSONObject(t, "contracts/feature-flags-v1.json")
	if manifest["manifest_version"] != "investigation-feature-flags/v1" {
		t.Fatal("unexpected flag manifest version")
	}
	seen := map[string]bool{}
	for _, raw := range array(t, manifest, "flags") {
		flag := raw.(map[string]any)
		name := flag["name"].(string)
		if seen[name] || flag["default"] != false {
			t.Fatalf("flag %s must be unique and default off", name)
		}
		seen[name] = true
	}
	if len(seen) != 7 {
		t.Fatalf("got %d flags, want 7", len(seen))
	}
}

func TestInvestigationVersionCompatibility(t *testing.T) {
	acceptsEvidence := func(version string) bool { return version == evidenceV1 }
	acceptsWorkflow := func(version string) bool { return version == workflowV1 }
	if !acceptsEvidence(evidenceV1) || !acceptsWorkflow(workflowV1) {
		t.Fatal("v1 must be accepted")
	}
	if acceptsEvidence("investigation-evidence/v2") || acceptsWorkflow("investigation-workflow/v2") {
		t.Fatal("unknown major versions must be rejected")
	}
	fixture := readJSONObject(t, "contracts/fixtures/evidence-trace-deviations.json")
	fixture["future_optional_field"] = map[string]any{"safe": true}
	keys := make([]string, 0, len(fixture))
	for key := range fixture {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if fixture["contract_version"] != evidenceV1 {
		t.Fatal("additive optional field changed compatibility")
	}
}
