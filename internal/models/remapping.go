package models

// RemappingPrecondition scopes logs_extract rules to matching lines.
type RemappingPrecondition struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Operator string `json:"operator"`
}

// RemappingLogsExtractProperties is the properties block for logs_extract rules.
type RemappingLogsExtractProperties struct {
	Type             string                   `json:"type"`
	Preconditions    []RemappingPrecondition  `json:"preconditions,omitempty"`
	RemapKeys        []string                 `json:"remap_keys"`
	TargetAttributes string                   `json:"target_attribute"`
	Action           string                   `json:"action"`
	Prefix           string                   `json:"prefix,omitempty"`
}

// RemappingLogsExtractRequest is the POST/PUT body for logs_extract rules.
type RemappingLogsExtractRequest struct {
	Name       string                         `json:"name"`
	Properties RemappingLogsExtractProperties `json:"properties"`
}

// RemappingMapProperties is the properties block for logs_map and traces_map rules.
type RemappingMapProperties struct {
	RemapKeys        []string `json:"remap_keys"`
	TargetAttributes string   `json:"target_attribute"`
}

// RemappingMapRequest is the POST/PUT body for logs_map and traces_map rules.
type RemappingMapRequest struct {
	Name       string                 `json:"name"`
	Properties RemappingMapProperties `json:"properties"`
}
