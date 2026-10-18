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
	var envelope struct {
		ContractVersion string `json:"contract_version"`
		AnalysisVersion string `json:"analysis_version"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return newTraceInvalidResponseError(err)
	}
	if envelope.ContractVersion != investigationEvidenceVersion {
		return newTraceInvalidResponseError(fmt.Errorf(
			"expected contract_version %q, got %q", investigationEvidenceVersion, envelope.ContractVersion))
	}
	if envelope.AnalysisVersion != analysisVersion {
		return newTraceInvalidResponseError(fmt.Errorf(
			"expected analysis_version %q, got %q", analysisVersion, envelope.AnalysisVersion))
	}
	return nil
}
