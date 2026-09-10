package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const schemaVersion = 1

// SourceContract is an operator-owned descriptor. Request arguments select a
// descriptor; they never supply fields, metric kinds, or record semantics.
type SourceContract struct {
	Datasource    string            `json:"datasource"`
	Source        string            `json:"source"`
	Index         string            `json:"index,omitempty"`
	SchemaVersion int               `json:"schema_version"`
	RecordUnit    string            `json:"record_unit,omitempty"`
	Fields        map[string]string `json:"fields,omitempty"`
	MetricKinds   map[string]string `json:"metric_kinds,omitempty"`
}

type contractKey struct {
	datasource string
	source     string
	index      string
	version    int
}

// Contracts is immutable after LoadContracts returns.
type Contracts struct {
	entries map[contractKey]SourceContract
}

func (c Contracts) Lookup(datasource, source, index string, version int) (SourceContract, bool) {
	contract, ok := c.entries[contractKey{datasource, source, index, version}]
	return cloneContract(contract), ok
}

// LoadContracts validates an operator-mounted JSON array once at startup.
func LoadContracts(path string) (Contracts, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Contracts{}, fmt.Errorf("read source contracts: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var items []SourceContract
	if err := decoder.Decode(&items); err != nil {
		return Contracts{}, fmt.Errorf("decode source contracts: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Contracts{}, fmt.Errorf("decode source contracts: unexpected trailing JSON")
	}
	entries := make(map[contractKey]SourceContract, len(items))
	for i, item := range items {
		item.Datasource = strings.TrimSpace(item.Datasource)
		item.Source = strings.TrimSpace(item.Source)
		item.Index = strings.TrimSpace(item.Index)
		if item.Datasource == "" || (item.Source != "logs" && item.Source != "traces") || item.SchemaVersion != schemaVersion {
			return Contracts{}, fmt.Errorf("source contracts[%d]: datasource, source, and schema_version=%d are required", i, schemaVersion)
		}
		if item.Source != "logs" && item.Index != "" {
			return Contracts{}, fmt.Errorf("source contracts[%d]: only logs contracts may set index", i)
		}
		if item.Source == "logs" && item.Index != "" {
			normalized, err := normalizeContractIndex(item.Index)
			if err != nil {
				return Contracts{}, fmt.Errorf("source contracts[%d]: invalid index: %w", i, err)
			}
			item.Index = normalized
		}
		for field, kind := range item.Fields {
			if strings.TrimSpace(field) == "" || strings.TrimSpace(kind) == "" {
				return Contracts{}, fmt.Errorf("source contracts[%d]: fields must use non-empty names and semantics", i)
			}
		}
		key := contractKey{item.Datasource, item.Source, item.Index, item.SchemaVersion}
		if _, duplicate := entries[key]; duplicate {
			return Contracts{}, fmt.Errorf("source contracts[%d]: duplicate datasource=%q source=%q index=%q schema_version=%d", i, item.Datasource, item.Source, item.Index, item.SchemaVersion)
		}
		entries[key] = cloneContract(item)
	}
	return Contracts{entries: entries}, nil
}

func normalizeContractIndex(index string) (string, error) {
	for _, prefix := range []string{"physical_index:", "rehydration_index:"} {
		if rest, ok := strings.CutPrefix(index, prefix); ok && strings.TrimSpace(rest) != "" {
			return index, nil
		}
	}
	if !strings.HasPrefix(index, "physical_index:") && !strings.HasPrefix(index, "rehydration_index:") {
		return "", fmt.Errorf("index must use physical_index:<name> or rehydration_index:<name>")
	}
	return "", fmt.Errorf("index must include a name")
}

func cloneContract(in SourceContract) SourceContract {
	out := in
	out.Fields = cloneStrings(in.Fields)
	out.MetricKinds = cloneStrings(in.MetricKinds)
	return out
}

func cloneStrings(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
