package traces

import (
	"bytes"
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
	if err := checkUniqueKeys(json.NewDecoder(bytes.NewReader(body))); err != nil {
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

// checkUniqueKeys fails on any object repeating a key. encoding/json keeps the
// last duplicate, so a first-key parser downstream could read a value never checked.
func checkUniqueKeys(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := keyTok.(string)
			if seen[key] {
				return fmt.Errorf("duplicate key %q", key)
			}
			seen[key] = true
			if err := checkUniqueKeys(dec); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for dec.More() {
			if err := checkUniqueKeys(dec); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = dec.Token() // the closing delimiter
	return err
}
