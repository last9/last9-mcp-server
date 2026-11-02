package traces

import (
	"encoding/json"
	"fmt"
)

// A body carrying this must satisfy contracts/investigation-evidence-v1.schema.json.
const investigationEvidenceVersion = "investigation-evidence/v1"

// Evidence bodies are forwarded verbatim, so a payload that is not the promised
// envelope would have the model reasoning over fields that do not exist.
func checkEvidenceContract(body []byte, analysisVersion string) error {
	_, err := readEvidenceEnvelope(body, analysisVersion)
	return err
}

// readEvidenceEnvelope returns the body's top-level fields once its versions
// match. Keys are read exactly: a struct decode would also accept
// "CONTRACT_VERSION", which a client reading "contract_version" never sees.
func readEvidenceEnvelope(body []byte, analysisVersion string) (map[string]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, newTraceInvalidResponseError(err)
	}
	for _, field := range [][2]string{{"contract_version", investigationEvidenceVersion}, {"analysis_version", analysisVersion}} {
		key, want := field[0], field[1]
		var got string
		_ = json.Unmarshal(top[key], &got) // absent or non-string leaves got empty
		if got != want {
			return nil, newTraceInvalidResponseError(fmt.Errorf("expected %s %q, got %q", key, want, got))
		}
	}
	return top, nil
}
