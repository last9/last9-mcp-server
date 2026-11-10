package traces

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
)

const evidenceSchemaPath = "../../../contracts/investigation-evidence-v1.schema.json"

func resolvedEvidenceSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(evidenceSchemaPath))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve evidence schema: %v", err)
	}
	return resolved
}

const waterfallFixturePath = "../../../contracts/fixtures/evidence-trace-waterfall.json"

func TestWaterfallFixtureMatchesServiceContract(t *testing.T) {
	if err := checkWaterfallEvidence(waterfallFixture(t)); err != nil {
		t.Fatalf("service fixture does not satisfy the waterfall contract: %v", err)
	}
}
