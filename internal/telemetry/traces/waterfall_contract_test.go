package traces

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
)

const evidenceSchemaPath = "../../../contracts/investigation-evidence-v1.schema.json"

func resolvedEvidenceSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(evidenceSchemaPath))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve evidence schema: %v", err)
	}
	return resolved
}

// The relayed body is what reaches the model, so hold it to the evidence schema.
func TestWaterfallFixtureSatisfiesEvidenceContract(t *testing.T) {
	call := callWaterfallHandler(t, http.StatusOK, waterfallFixture(t), GetTraceWaterfallArgs{TraceID: testValidTraceID})
	if call.err != nil {
		t.Fatal(call.err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(waterfallText(t, call.result)), &payload); err != nil {
		t.Fatal(err)
	}
	if err := resolvedEvidenceSchema(t).Validate(payload); err != nil {
		t.Fatalf("relayed waterfall does not satisfy %s: %v", evidenceSchemaPath, err)
	}
	// contracts/README.md forbids causal claims from this producer.
	if payload["interpretation"].(map[string]any)["claim_type"] == "cause" {
		t.Fatal("causal claim type is forbidden")
	}
}
