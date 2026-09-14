package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

const schemaVersion = 1

// SourceContract is a Last9-managed descriptor. Request arguments select a
// descriptor; they never supply fields, metric kinds, or record semantics.
type SourceContract struct {
	Datasource    string               `json:"datasource"`
	Source        string               `json:"source"`
	Index         string               `json:"index,omitempty"`
	SchemaVersion int                  `json:"schema_version"`
	RecordUnit    string               `json:"record_unit,omitempty"`
	Fields        map[string]string    `json:"fields,omitempty"`
	MetricKinds   map[string]string    `json:"metric_kinds,omitempty"`
	BackendLimits BackendLimits        `json:"backend_limits"`
	Execution     *ExecutionDescriptor `json:"execution,omitempty"`
}

// ExecutionDescriptor grants semantics only from the Last9 API contract.
// Observed fields and catalog arguments cannot create this section.
type ExecutionDescriptor struct {
	RecordUnit       string             `json:"record_unit,omitempty"`
	StatusField      string             `json:"status_field,omitempty"`
	StatusCoverage   string             `json:"status_coverage,omitempty"`
	ParserStages     []ParserStage      `json:"parser_stages"`
	DimensionFields  map[string]string  `json:"dimension_fields,omitempty"`
	MetricKind       string             `json:"metric_kind,omitempty"`
	MetricUnit       string             `json:"metric_unit,omitempty"`
	CadenceSeconds   int                `json:"cadence_seconds,omitempty"`
	EnvironmentField string             `json:"environment_field,omitempty"`
	GraphQL          *GraphQLDescriptor `json:"graphql,omitempty"`
}

type ParserStage struct {
	Type    string            `json:"type"`
	Parser  string            `json:"parser"`
	Field   string            `json:"field,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	Pattern string            `json:"pattern,omitempty"`
}

// Qualified attests execution identity, complete errors, and a top-level
// server-owned population excluding resolver and subscription delivery spans.
type GraphQLDescriptor struct {
	Qualified                bool   `json:"qualified"`
	ExecutionIDField         string `json:"execution_id_field"`
	OperationTypeField       string `json:"operation_type_field"`
	OperationIdentityField   string `json:"operation_identity_field"`
	IdentityKind             string `json:"identity_kind"`
	ErrorCountField          string `json:"error_count_field"`
	PreExecutionFailureField string `json:"pre_execution_failure_field"`
	PopulationField          string `json:"population_field"`
	PopulationValue          string `json:"population_value"`
	Unit                     string `json:"unit"`
}

var descriptorField = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_]*|(?:attributes|resources)\['[^'\\\r\n]+'\])$`)
var descriptorName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

func validDescriptorText(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}

func validateExecution(c SourceContract) error {
	e := c.Execution
	if e == nil {
		return nil
	}
	if c.BackendLimits.MaxRows < 5000 {
		return fmt.Errorf("execution requires backend max_rows >= 5000")
	}
	if e.ParserStages == nil || len(e.ParserStages) > 8 || len(e.DimensionFields) > 16 {
		return fmt.Errorf("invalid execution parsers or dimensions")
	}
	if c.Source == "logs" && !descriptorName.MatchString(e.RecordUnit) {
		return fmt.Errorf("execution requires log record_unit")
	}
	if e.RecordUnit != "" && (len(e.RecordUnit) > 64 || !descriptorName.MatchString(e.RecordUnit)) {
		return fmt.Errorf("invalid record_unit")
	}
	if c.RecordUnit != "" && c.RecordUnit != e.RecordUnit {
		return fmt.Errorf("conflicting record_unit")
	}
	if (e.StatusField == "") != (e.StatusCoverage == "") || (e.StatusField != "" && e.StatusCoverage != "complete") {
		return fmt.Errorf("status_field requires complete status_coverage")
	}
	for _, field := range []string{e.StatusField, e.EnvironmentField} {
		if field != "" && (!validDescriptorText(field) || !descriptorField.MatchString(field)) {
			return fmt.Errorf("invalid execution field")
		}
	}
	for name, field := range e.DimensionFields {
		if len(name) > 64 || !descriptorName.MatchString(name) || !validDescriptorText(field) || !descriptorField.MatchString(field) {
			return fmt.Errorf("invalid dimension field")
		}
	}
	if c.Source != "logs" && len(e.ParserStages) != 0 {
		return fmt.Errorf("only logs may parse records")
	}
	for _, stage := range e.ParserStages {
		if stage.Type != "parse" || (stage.Parser != "json" && stage.Parser != "logfmt" && stage.Parser != "regexp") {
			return fmt.Errorf("invalid execution parser")
		}
		if stage.Field != "" && (!validDescriptorText(stage.Field) || !descriptorField.MatchString(stage.Field)) {
			return fmt.Errorf("invalid parser field")
		}
		if len(stage.Labels) > 32 {
			return fmt.Errorf("too many parser labels")
		}
		for key, value := range stage.Labels {
			if !validDescriptorText(key) || !validDescriptorText(value) {
				return fmt.Errorf("invalid parser labels")
			}
		}
		if stage.Parser == "regexp" {
			if len(stage.Pattern) == 0 || len(stage.Pattern) > 4096 {
				return fmt.Errorf("invalid parser pattern")
			}
			if _, err := regexp.Compile(stage.Pattern); err != nil {
				return fmt.Errorf("invalid parser pattern")
			}
		} else if stage.Pattern != "" {
			return fmt.Errorf("pattern requires regexp")
		}
	}
	if e.MetricKind != "" || e.MetricUnit != "" || e.CadenceSeconds != 0 {
		if (e.MetricKind != "counter" && e.MetricKind != "per_window_gauge" && e.MetricKind != "exact_distribution") || !validDescriptorText(e.MetricUnit) || e.CadenceSeconds < 1 {
			return fmt.Errorf("invalid metric semantics")
		}
	}
	if g := e.GraphQL; g != nil {
		if !g.Qualified || (g.IdentityKind != "name" && g.IdentityKind != "persisted_id") || (g.Unit != "operation_attempt" && g.Unit != "subscription_establishment") || !validDescriptorText(g.PopulationValue) {
			return fmt.Errorf("unqualified graphql semantics")
		}
		for _, field := range []string{g.ExecutionIDField, g.OperationTypeField, g.OperationIdentityField, g.ErrorCountField, g.PreExecutionFailureField, g.PopulationField} {
			if !validDescriptorText(field) || !descriptorField.MatchString(field) {
				return fmt.Errorf("invalid graphql field")
			}
		}
	}
	return nil
}

// BackendLimits is the Last9-managed adapter attestation required before a
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

// Contracts is immutable after DecodeContracts returns.
type Contracts struct {
	entries map[contractKey]SourceContract
}

// ExactQuantileAuthorizer checks the managed API contract at execution time.
type ExactQuantileAuthorizer struct {
	client *http.Client
	cfg    models.Config
}

func NewExactQuantileAuthorizer(client *http.Client, cfg models.Config) *ExactQuantileAuthorizer {
	return &ExactQuantileAuthorizer{client: client, cfg: cfg}
}

func (a *ExactQuantileAuthorizer) AllowsExactLogQuantile(ctx context.Context, datasource, index, field string) (bool, error) {
	if datasource == "" {
		for _, candidate := range a.cfg.Datasources {
			if candidate.IsDefault {
				datasource = candidate.Name
				break
			}
		}
	}
	contracts, err := FetchContracts(ctx, a.client, a.cfg, datasource, index)
	if err != nil {
		return false, err
	}
	return contracts.AllowsExactLogQuantile(datasource, index, field), nil
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

// DecodeContracts validates contracts received from Last9 API.
func DecodeContracts(contents []byte) (Contracts, error) {
	if err := rejectDuplicateKeys(json.NewDecoder(bytes.NewReader(contents))); err != nil {
		return Contracts{}, fmt.Errorf("decode source contracts: %w", err)
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
		if err := validateExecution(item); err != nil {
			return Contracts{}, fmt.Errorf("source contracts[%d]: %w", i, err)
		}
		key := contractKey{item.Datasource, item.Source, item.Index, item.SchemaVersion}
		if _, duplicate := entries[key]; duplicate {
			return Contracts{}, fmt.Errorf("source contracts[%d]: duplicate datasource=%q source=%q index=%q schema_version=%d", i, item.Datasource, item.Source, item.Index, item.SchemaVersion)
		}
		entries[key] = cloneContract(item)
	}
	return Contracts{entries: entries}, nil
}

func FetchContracts(ctx context.Context, client *http.Client, cfg models.Config, datasource, index string) (Contracts, error) {
	ds, ok := cfg.ResolveDatasource(datasource)
	if !ok || ds.ID == "" {
		return Contracts{}, fmt.Errorf("datasource %q has no API identity", datasource)
	}
	if cfg.TokenManager == nil {
		return Contracts{}, fmt.Errorf("source contracts require an access token")
	}
	query := url.Values{"schema_version": {strconv.Itoa(schemaVersion)}}
	if index != "" {
		query.Set("index", index)
	}
	path := fmt.Sprintf("/datasources/%s/api-source-contracts/?%s", url.PathEscape(ds.ID), query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.APIBaseURL+path, nil)
	if err != nil {
		return Contracts{}, err
	}
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
	resp, err := client.Do(req)
	if err != nil {
		return Contracts{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Contracts{}, fmt.Errorf("source contracts returned status %d", resp.StatusCode)
	}
	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return Contracts{}, err
	}
	return DecodeContracts(contents)
}

func rejectDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate object key")
			}
			seen[name] = true
		}
		if err := rejectDuplicateKeys(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func normalizeContractIndex(index string) (string, error) {
	return utils.NormalizeLogIndex(index)
}

func cloneContract(in SourceContract) SourceContract {
	out := in
	out.Fields = cloneStrings(in.Fields)
	out.MetricKinds = cloneStrings(in.MetricKinds)
	if in.Execution != nil {
		e := *in.Execution
		e.DimensionFields = cloneStrings(e.DimensionFields)
		e.ParserStages = make([]ParserStage, len(in.Execution.ParserStages))
		for i, stage := range in.Execution.ParserStages {
			e.ParserStages[i] = stage
			e.ParserStages[i].Labels = cloneStrings(stage.Labels)
		}
		if e.GraphQL != nil {
			g := *e.GraphQL
			e.GraphQL = &g
		}
		out.Execution = &e
	}
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
