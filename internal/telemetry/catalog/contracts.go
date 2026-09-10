package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"last9-mcp/internal/utils"
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
	BackendLimits BackendLimits     `json:"backend_limits"`
}

// BackendLimits is the operator-owned adapter attestation required before a
// bounded response can be complete.
type BackendLimits struct {
	NoHiddenSampling bool `json:"no_hidden_sampling"`
	MaxRows          int  `json:"max_rows"`
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

// AllowsExactLogQuantile is consumed by get_logs through models' narrow
// interface; request arguments never grant exact aggregate support.
func (c Contracts) AllowsExactLogQuantile(datasource, index, field string) bool {
	contract, ok := c.Lookup(datasource, "logs", index, schemaVersion)
	return ok && contract.MetricKinds[field] == "exact_quantile"
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
		if !item.BackendLimits.NoHiddenSampling || item.BackendLimits.MaxRows < 1 {
			return Contracts{}, fmt.Errorf("source contracts[%d]: backend_limits must attest no_hidden_sampling with a positive max_rows", i)
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
	return utils.NormalizeLogIndex(index)
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
