// Package catalog exposes bounded, evidence-qualified source discovery.
package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	logtelemetry "last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxCatalogLimit = 100

type CatalogArgs struct {
	Datasource   string   `json:"datasource" jsonschema:"(Required) Cached datasource name to inspect."`
	Sources      []string `json:"sources" jsonschema:"(Required) Sources to inspect: logs and/or traces."`
	LogIndex     string   `json:"log_index,omitempty" jsonschema:"Optional log index in physical_index:<name> or rehydration_index:<block_name> form."`
	Protocol     string   `json:"protocol" jsonschema:"(Required) Protocol context for this catalog request."`
	StartTimeISO string   `json:"start_time_iso" jsonschema:"(Required) Inclusive RFC3339 start time."`
	EndTimeISO   string   `json:"end_time_iso" jsonschema:"(Required) Exclusive RFC3339 end time."`
	Include      []string `json:"include" jsonschema:"(Required) Sections to include: services, environments, fields."`
	Limit        int      `json:"limit,omitempty" jsonschema:"Maximum services or environments per source; default 100, maximum 100."`
}

type Scope struct {
	Datasource   string   `json:"datasource"`
	Sources      []string `json:"sources"`
	LogIndex     string   `json:"log_index,omitempty"`
	Protocol     string   `json:"protocol"`
	StartTimeISO string   `json:"start_time_iso"`
	EndTimeISO   string   `json:"end_time_iso"`
}

type ObservedValue struct {
	Source string `json:"source"`
	Field  string `json:"field"`
	Value  string `json:"value"`
	Count  int64  `json:"count"`
}

type FieldEvidence struct {
	Source      string         `json:"source"`
	Field       string         `json:"field"`
	Provenance  string         `json:"provenance"`
	Parser      string         `json:"parser,omitempty"`
	SampleCount int            `json:"sample_count,omitempty"`
	Coverage    float64        `json:"coverage,omitempty"`
	Extraction  map[string]any `json:"extraction,omitempty"`
	Complete    bool           `json:"complete"`
}

type ResultEnvelope struct {
	Partial      bool   `json:"partial"`
	Reason       string `json:"reason,omitempty"`
	ReturnedRows int    `json:"returned_rows,omitempty"`
	RowLimit     int    `json:"row_limit,omitempty"`
	OverFetched  bool   `json:"over_fetched,omitempty"`
}

type CatalogResponse struct {
	SchemaVersion int              `json:"schema_version"`
	Requested     Scope            `json:"requested"`
	Effective     Scope            `json:"effective"`
	Services      []ObservedValue  `json:"services"`
	Environments  []ObservedValue  `json:"environments,omitempty"`
	Fields        []FieldEvidence  `json:"fields"`
	Descriptors   []SourceContract `json:"descriptors,omitempty"`
	Result        ResultEnvelope   `json:"l9_result"`
}

func NewHandler(client *http.Client, cfg models.Config, contracts Contracts) func(context.Context, *mcp.CallToolRequest, CatalogArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CatalogArgs) (*mcp.CallToolResult, any, error) {
		requested, queryCfg, start, end, includes, err := validateArgs(args, cfg)
		if err != nil {
			return nil, nil, err
		}
		response := CatalogResponse{
			SchemaVersion: schemaVersion,
			Requested:     requested,
			Effective:     requested,
			Services:      []ObservedValue{},
			Fields:        []FieldEvidence{},
		}
		var reasons []string
		for _, source := range requested.Sources {
			index := ""
			if source == "logs" {
				index = requested.LogIndex
			}
			contract, trusted := contracts.Lookup(requested.Datasource, source, index, schemaVersion)
			if trusted && contract.Execution != nil {
				response.Descriptors = append(response.Descriptors, contract)
			}
			fields, samples, fieldPartial, fieldReason, err := fetchFields(ctx, client, queryCfg, source, start, end, index)
			if err != nil {
				reasons = append(reasons, source+": "+err.Error())
				continue
			}
			if includes["fields"] {
				sourceFields := evidenceFor(source, fields, samples, contract, trusted, fieldPartial)
				if source == "logs" && needsBodyEvidence(contract) {
					discovery, err := logtelemetry.DiscoverLogAttributesForCatalogWithStatus(ctx, client, queryCfg, start/1000, end/1000, index)
					if err != nil {
						fieldPartial = true
						fieldReason = "body field discovery: " + err.Error()
						sourceFields = appendMissingBodyEvidence(sourceFields, contract, nil)
					} else {
						if discovery.Partial {
							fieldPartial = true
							fieldReason = discovery.Reason
						}
						bodyAttributes := bodyDerivedAttributes(discovery.Attributes)
						sourceFields = mergeFieldEvidence(sourceFields, logEvidence(bodyAttributes, contract, fieldPartial, discovery.BodySampleCount))
						sourceFields = appendMissingBodyEvidence(sourceFields, contract, bodyAttributes)
					}
				}
				response.Fields = append(response.Fields, sourceFields...)
			}
			if fieldPartial {
				reasons = append(reasons, source+": "+fieldReason)
			}
			if !trusted {
				reasons = append(reasons, source+": missing trusted source contract")
				continue // observations are useful; counts are contract-dependent conclusions.
			}
			if includes["services"] {
				values, inventory, err := fetchInventory(ctx, client, queryCfg, source, start, end, index, "ServiceName", args.Limit, contract)
				if err != nil {
					reasons = append(reasons, source+": "+err.Error())
				} else {
					response.Services = append(response.Services, values...)
					response.Result.ReturnedRows += inventory.ReturnedRows
					response.Result.RowLimit = inventory.RowLimit
					response.Result.OverFetched = response.Result.OverFetched || inventory.OverFetched
					if inventory.Partial {
						reasons = append(reasons, source+": "+inventory.Reason)
					}
				}
			}
			if includes["environments"] {
				fields := environmentFields(contract)
				if len(fields) == 0 {
					reasons = append(reasons, source+": no configured environment field")
					continue
				}
				for _, field := range fields {
					values, inventory, err := fetchInventory(ctx, client, queryCfg, source, start, end, index, field, args.Limit, contract)
					if err != nil {
						reasons = append(reasons, source+": "+err.Error())
					} else {
						response.Environments = append(response.Environments, values...)
						response.Result.ReturnedRows += inventory.ReturnedRows
						response.Result.RowLimit = inventory.RowLimit
						response.Result.OverFetched = response.Result.OverFetched || inventory.OverFetched
						if inventory.Partial {
							reasons = append(reasons, source+": "+inventory.Reason)
						}
					}
				}
			}
		}
		if len(reasons) > 0 {
			response.Result.Partial = true
			response.Result.Reason = strings.Join(reasons, "; ")
		}
		body, err := json.Marshal(response)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal catalog response: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

func validateArgs(args CatalogArgs, cfg models.Config) (Scope, models.Config, int64, int64, map[string]bool, error) {
	args.Datasource = strings.TrimSpace(args.Datasource)
	args.Protocol = strings.TrimSpace(args.Protocol)
	if args.Datasource == "" || args.Protocol == "" || len(args.Sources) == 0 || args.StartTimeISO == "" || args.EndTimeISO == "" {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("datasource, sources, protocol, start_time_iso, and end_time_iso are required")
	}
	startAt, err := utils.ParseToolTimestamp(args.StartTimeISO)
	if err != nil {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("invalid start_time_iso: %w", err)
	}
	endAt, err := utils.ParseToolTimestamp(args.EndTimeISO)
	if err != nil {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("invalid end_time_iso: %w", err)
	}
	if !startAt.Before(endAt) {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("start_time_iso must be before end_time_iso")
	}
	if startAt.Nanosecond() != 0 || endAt.Nanosecond() != 0 {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("subsecond bounds are not representable by this backend")
	}
	index, err := utils.NormalizeLogIndex(args.LogIndex)
	if err != nil {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("invalid log_index: %w", err)
	}
	seenSources := map[string]bool{}
	sources := make([]string, 0, len(args.Sources))
	for _, raw := range args.Sources {
		source := strings.ToLower(strings.TrimSpace(raw))
		if source != "logs" && source != "traces" {
			return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("unsupported source %q; use logs and/or traces", raw)
		}
		if !seenSources[source] {
			seenSources[source] = true
			sources = append(sources, source)
		}
	}
	if index != "" && !seenSources["logs"] {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("log_index requires logs in sources")
	}
	includes := map[string]bool{}
	for _, raw := range args.Include {
		item := strings.ToLower(strings.TrimSpace(raw))
		if item != "services" && item != "environments" && item != "fields" {
			return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("unsupported include %q", raw)
		}
		includes[item] = true
	}
	if len(includes) == 0 {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("include is required")
	}
	if args.Limit < 0 || args.Limit > maxCatalogLimit {
		return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("limit must be between 1 and %d", maxCatalogLimit)
	}
	queryCfg := cfg
	if args.Datasource != cfg.DatasourceName {
		ds, ok := cfg.ResolveDatasource(args.Datasource)
		if !ok {
			return Scope{}, models.Config{}, 0, 0, nil, fmt.Errorf("datasource %q is not configured", args.Datasource)
		}
		queryCfg.Region = ds.Region
	}
	return Scope{Datasource: args.Datasource, Sources: sources, LogIndex: index, Protocol: args.Protocol, StartTimeISO: startAt.Format("2006-01-02T15:04:05Z"), EndTimeISO: endAt.Format("2006-01-02T15:04:05Z")}, queryCfg, startAt.UnixMilli(), endAt.UnixMilli(), includes, nil
}

func fetchFields(ctx context.Context, client *http.Client, cfg models.Config, source string, start, end int64, index string) ([]string, int, bool, string, error) {
	endpoint := constants.EndpointTracesSeries
	if source == "logs" {
		endpoint = constants.EndpointLogsSeries
	}
	params := url.Values{"region": []string{cfg.Region}, "start": []string{strconv.FormatInt(start/1000, 10)}, "end": []string{strconv.FormatInt(end/1000, 10)}}
	if index != "" {
		params.Set("index", index)
	}
	reqBody, _ := json.Marshal(map[string]any{"pipeline": []map[string]any{}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+endpoint+"?"+params.Encode(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, false, "", err
	}
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, false, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, false, "", fmt.Errorf("series returned status %d", resp.StatusCode)
	}
	var payload struct {
		Data   []map[string]json.RawMessage `json:"data"`
		Status string                       `json:"status"`
		Result json.RawMessage              `json:"l9_result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, 0, false, "", err
	}
	if payload.Status != "success" {
		return nil, 0, false, "", fmt.Errorf("series returned status %q", payload.Status)
	}
	seen := map[string]bool{}
	for _, row := range payload.Data {
		for field := range row {
			if field != "" {
				seen[field] = true
			}
		}
	}
	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	envelope, err := parseBackendEnvelope(payload.Result)
	if err != nil {
		return fields, len(payload.Data), true, "invalid backend attestation: " + err.Error(), nil
	}
	reason := envelope.Reason
	if envelope.Partial && reason == "" {
		reason = "series response is partial"
	}
	return fields, len(payload.Data), envelope.Partial, reason, nil
}

func parseBackendEnvelope(raw json.RawMessage) (ResultEnvelope, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return ResultEnvelope{}, fmt.Errorf("missing partial boolean")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ResultEnvelope{}, fmt.Errorf("invalid envelope")
	}
	partialRaw, ok := payload["partial"]
	if !ok || string(partialRaw) == "null" {
		return ResultEnvelope{}, fmt.Errorf("missing partial boolean")
	}
	var partial bool
	if err := json.Unmarshal(partialRaw, &partial); err != nil {
		return ResultEnvelope{}, fmt.Errorf("invalid partial boolean")
	}
	result := ResultEnvelope{Partial: partial}
	if reasonRaw, ok := payload["reason"]; ok && string(reasonRaw) != "null" {
		if err := json.Unmarshal(reasonRaw, &result.Reason); err != nil {
			return ResultEnvelope{}, fmt.Errorf("invalid partial reason")
		}
	}
	return result, nil
}

func fetchInventory(ctx context.Context, client *http.Client, cfg models.Config, source string, start, end int64, index, field string, requestedLimit int, contract SourceContract) ([]ObservedValue, ResultEnvelope, error) {
	limit := requestedLimit
	if limit == 0 {
		limit = maxCatalogLimit
	}
	pipeline := []map[string]any{{"type": "aggregate", "aggregates": []map[string]any{{"function": map[string]any{"$count": []any{}}, "as": "count"}}, "groupby": map[string]any{field: "value"}}}
	var resp *http.Response
	var err error
	if contract.BackendLimits.MaxRows < limit+1 {
		return nil, ResultEnvelope{Partial: true, Reason: "backend max_rows cannot attest requested over-fetch", RowLimit: limit}, nil
	}
	if source == "logs" {
		resp, err = utils.MakeLogsJSONQueryAPI(ctx, client, cfg, pipeline, start, end, limit+1, index)
	} else {
		resp, err = utils.MakeTracesJSONQueryAPI(ctx, client, cfg, pipeline, start, end, limit+1)
	}
	if err != nil {
		return nil, ResultEnvelope{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ResultEnvelope{}, fmt.Errorf("inventory returned status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, ResultEnvelope{}, err
	}
	if status, ok := payload["status"].(string); !ok || status != "success" {
		return nil, ResultEnvelope{}, fmt.Errorf("inventory returned non-success status")
	}
	envelope := ResultEnvelope{RowLimit: limit, OverFetched: true}
	rawEnvelope, err := json.Marshal(payload["l9_result"])
	if err != nil {
		return nil, ResultEnvelope{Partial: true, Reason: "invalid backend attestation", RowLimit: limit}, nil
	}
	upstream, err := parseBackendEnvelope(rawEnvelope)
	if err != nil {
		return nil, ResultEnvelope{Partial: true, Reason: "invalid backend attestation: " + err.Error(), RowLimit: limit}, nil
	}
	if upstream.Partial {
		envelope.Partial = true
		envelope.Reason = upstream.Reason
		if envelope.Reason == "" {
			envelope.Reason = "backend returned partial inventory"
		}
		return nil, envelope, nil
	}
	data, _ := payload["data"].(map[string]any)
	rows, ok := data["result"].([]any)
	if !ok {
		return nil, ResultEnvelope{}, fmt.Errorf("inventory response missing result array")
	}
	if len(rows) > limit {
		rows = rows[:limit]
		envelope.Partial, envelope.Reason = true, "row_limit_reached"
	}
	values := make([]ObservedValue, 0, len(rows))
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			envelope.Partial, envelope.Reason = true, "malformed inventory row"
			continue
		}
		if metric, wrapped := item["metric"].(map[string]any); wrapped {
			item = metric
		}
		value, _ := item["value"].(string)
		if value == "" {
			value, _ = item[field].(string)
		}
		count, ok := number(item["count"])
		if !ok || count < 0 || value == "" {
			envelope.Partial, envelope.Reason = true, "malformed inventory row"
			continue
		}
		values = append(values, ObservedValue{Source: source, Field: field, Value: value, Count: count})
	}
	envelope.ReturnedRows = len(values)
	return values, envelope, nil
}

func environmentFields(contract SourceContract) []string {
	var fields []string
	for field, kind := range contract.MetricKinds {
		if kind == "environment" {
			fields = append(fields, field)
		}
	}
	sort.Strings(fields)
	return fields
}

func number(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if math.Trunc(v) == v && v >= math.MinInt64 && v <= math.MaxInt64 {
			return int64(v), true
		}
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func evidenceFor(source string, observed []string, samples int, contract SourceContract, trusted, partial bool) []FieldEvidence {
	seen := map[string]bool{}
	out := make([]FieldEvidence, 0, len(observed)+len(contract.Fields))
	for _, field := range observed {
		seen[field] = true
		provenance, complete := "observed", false
		if trusted && !partial {
			if descriptor, configured := contract.Fields[field]; configured {
				provenance, complete = descriptor, descriptor != "body"
			}
		}
		out = append(out, FieldEvidence{Source: source, Field: field, Provenance: provenance, SampleCount: samples, Complete: complete})
	}
	if trusted && !partial {
		for field, descriptor := range contract.Fields {
			if seen[field] {
				continue
			}
			if descriptor == "body" {
				continue
			}
			out = append(out, FieldEvidence{Source: source, Field: field, Provenance: descriptor, Complete: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

func needsBodyEvidence(contract SourceContract) bool {
	for _, descriptor := range contract.Fields {
		if descriptor == "body" {
			return true
		}
	}
	return false
}

func logEvidence(attributes []logtelemetry.LogAttribute, contract SourceContract, partial bool, sampleCount int) []FieldEvidence {
	out := make([]FieldEvidence, 0, len(attributes))
	for _, attribute := range attributes {
		provenance := attribute.Source
		if provenance == "" {
			provenance = "indexed"
		}
		coverage := 0.0
		if parts := strings.Split(attribute.SampleCoverage, "/"); len(parts) == 2 {
			numerator, a := strconv.ParseFloat(parts[0], 64)
			denominator, b := strconv.ParseFloat(parts[1], 64)
			if a == nil && b == nil && denominator > 0 {
				coverage = numerator / denominator
			}
		}
		parser := ""
		var extraction map[string]any
		if provenance == "body" {
			var stages []map[string]any
			if json.Unmarshal([]byte(attribute.Hint), &stages) == nil && len(stages) > 0 {
				parser, _ = stages[0]["parser"].(string)
				extraction = stages[0]
			}
		}
		_, configured := contract.Fields[attribute.Name]
		out = append(out, FieldEvidence{Source: "logs", Field: attribute.FilterField, Provenance: provenance, Parser: parser, SampleCount: sampleCount, Coverage: coverage, Extraction: extraction, Complete: configured && provenance == "indexed" && !partial})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

func mergeFieldEvidence(base, additional []FieldEvidence) []FieldEvidence {
	byKey := make(map[string]int, len(base)+len(additional))
	for i, evidence := range base {
		byKey[evidence.Source+"\x00"+evidence.Field] = i
	}
	for _, evidence := range additional {
		key := evidence.Source + "\x00" + evidence.Field
		if i, ok := byKey[key]; ok {
			base[i] = evidence
			continue
		}
		byKey[key] = len(base)
		base = append(base, evidence)
	}
	sort.Slice(base, func(i, j int) bool {
		if base[i].Source != base[j].Source {
			return base[i].Source < base[j].Source
		}
		return base[i].Field < base[j].Field
	})
	return base
}

func appendMissingBodyEvidence(base []FieldEvidence, contract SourceContract, attributes []logtelemetry.LogAttribute) []FieldEvidence {
	seen := make(map[string]bool, len(attributes))
	for _, attribute := range attributes {
		if attribute.Source == "body" {
			seen[attribute.Name] = true
		}
	}
	for field, descriptor := range contract.Fields {
		if descriptor == "body" && !seen[field] {
			base = append(base, FieldEvidence{Source: "logs", Field: field, Provenance: "body", Complete: false})
		}
	}
	return mergeFieldEvidence(base, nil)
}

func bodyDerivedAttributes(attributes []logtelemetry.LogAttribute) []logtelemetry.LogAttribute {
	body := make([]logtelemetry.LogAttribute, 0, len(attributes))
	for _, attribute := range attributes {
		if attribute.Source == "body" {
			body = append(body, attribute)
		}
	}
	return body
}
