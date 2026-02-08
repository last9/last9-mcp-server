package knowledge

import (
	"encoding/csv"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// FormatType represents the detected format of raw input text.
type FormatType int

const (
	FormatUnknown   FormatType = iota
	FormatJSON                 // Valid JSON object or array
	FormatYAML                 // Valid YAML (map or slice, not bare string)
	FormatCSV                  // Comma-separated values with consistent field count
	FormatPlainText            // Unstructured text (logs, prose)
)

func (f FormatType) String() string {
	switch f {
	case FormatJSON:
		return "JSON"
	case FormatYAML:
		return "YAML"
	case FormatCSV:
		return "CSV"
	case FormatPlainText:
		return "PlainText"
	default:
		return "Unknown"
	}
}

// DetectFormat determines the format of raw input and returns the parsed value
// for structured formats. Detection order: JSON > YAML > CSV > PlainText.
// JSON is tried before YAML because YAML is a superset of JSON.
func DetectFormat(raw string) (FormatType, interface{}, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return FormatUnknown, nil, nil
	}

	// JSON: starts with { or [
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return FormatJSON, parsed, nil
		}
	}

	// YAML: try to unmarshal. Result must be map or slice, not a bare scalar.
	var yamlParsed interface{}
	if err := yaml.Unmarshal([]byte(trimmed), &yamlParsed); err == nil {
		switch yamlParsed.(type) {
		case map[string]interface{}:
			return FormatYAML, yamlParsed, nil
		case []interface{}:
			return FormatYAML, yamlParsed, nil
		}
	}

	// CSV: first line has commas, >=3 lines with consistent field count
	if detectCSV(trimmed) {
		reader := csv.NewReader(strings.NewReader(trimmed))
		records, err := reader.ReadAll()
		if err == nil && len(records) >= 3 {
			return FormatCSV, records, nil
		}
	}

	return FormatPlainText, nil, nil
}

// detectCSV checks if text looks like CSV: first line has commas and
// at least 3 lines have a consistent number of fields.
func detectCSV(text string) bool {
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		return false
	}

	// First line must contain commas
	if !strings.Contains(lines[0], ",") {
		return false
	}

	reader := csv.NewReader(strings.NewReader(text))
	records, err := reader.ReadAll()
	if err != nil {
		return false
	}
	if len(records) < 3 {
		return false
	}

	// All rows must have the same field count
	fieldCount := len(records[0])
	if fieldCount < 2 {
		return false
	}
	for _, row := range records[1:] {
		if len(row) != fieldCount {
			return false
		}
	}
	return true
}
