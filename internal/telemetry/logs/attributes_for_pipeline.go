package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetLogAttributesForPipelineArgs represents the input arguments for the
// get_log_attributes_for_pipeline tool.
type GetLogAttributesForPipelineArgs struct {
	Pipeline        []map[string]interface{} `json:"pipeline,omitempty" jsonschema:"Pipeline of prior filter stages to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}] (required)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string                   `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
	Index           string                   `json:"index,omitempty" jsonschema:"Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit this when the user did not specify an index."`
}

// logSeriesResponse represents the /logs/api/v2/series/json response. Each entry
// in Data is a label-set; we only need its keys (the field names present).
type logSeriesResponse struct {
	Data   []map[string]json.RawMessage `json:"data"`
	Status string                       `json:"status"`
}

// LogAttribute is an enriched attribute entry returned by
// get_log_attributes_for_pipeline. filter_field is the exact string to use in a
// logjson filter condition. Body-derived entries (source "body") exist only
// inside the log Body as JSON: their filter_field is valid only after the parse
// stage shown in the hint, and sample_coverage reports in how many of the
// sampled rows the key appeared.
type LogAttribute struct {
	Name           string `json:"name"`
	FilterField    string `json:"filter_field"`
	Hint           string `json:"hint"`
	Source         string `json:"source,omitempty"`
	SampleCoverage string `json:"sample_coverage,omitempty"`
}

const (
	// bodySampleLimit bounds the raw-log sample used to discover Body-derived keys.
	bodySampleLimit = 5
	// maxBodyDerivedKeys caps how many Body-derived keys are reported, ranked by
	// sample frequency — a wide structured Body must not flood the response.
	maxBodyDerivedKeys = 20
)

// safeBodyKeyPattern accepts only keys that embed safely into both the JSON
// hint and the attributes['<key>'] accessor. Keys with quotes, spaces, or
// backslashes would emit broken hints the model copies verbatim — skip them.
var safeBodyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@/-]*$`)

// bodyDerivedHint renders the two-stage example (parse Body, then filter on the
// materialized key) via json.Marshal so key content can never break the JSON.
func bodyDerivedHint(key string) string {
	stages := []map[string]interface{}{
		{"type": "parse", "parser": "json", "field": "Body", "labels": map[string]string{key: key}},
		{"type": "filter", "query": map[string]interface{}{"$eq": []string{fmt.Sprintf("attributes['%s']", key), "<value>"}}},
	}
	b, err := json.Marshal(stages)
	if err != nil {
		return ""
	}
	return string(b)
}

// logfmtBodyDerivedHint renders the two-stage example for a Body that is
// logfmt-encoded (key=value pairs) rather than JSON.
func logfmtBodyDerivedHint(key string) string {
	stages := []map[string]interface{}{
		{"type": "parse", "parser": "logfmt", "field": "Body"},
		{"type": "filter", "query": map[string]interface{}{"$eq": []string{fmt.Sprintf("attributes['%s']", key), "<value>"}}},
	}
	b, err := json.Marshal(stages)
	if err != nil {
		return ""
	}
	return string(b)
}

// regexpBodyDerivedHint renders the two-stage example for a Body field
// extracted via a regexp capture group (plaintext logs with no structured
// encoding, e.g. an inline severity token).
func regexpBodyDerivedHint(pattern, key, exampleValue string) string {
	stages := []map[string]interface{}{
		{"type": "parse", "parser": "regexp", "field": "Body", "pattern": pattern},
		{"type": "filter", "query": map[string]interface{}{"$eq": []string{fmt.Sprintf("attributes['%s']", key), exampleValue}}},
	}
	b, err := json.Marshal(stages)
	if err != nil {
		return ""
	}
	return string(b)
}

// logfmtKVPattern extracts key=value pairs from a logfmt-style line, e.g.
// `level=info msg="hi there" status=200`. Classification also requires the
// matched pairs to dominate the line (see isLogfmtLine) so incidental
// `a=1 … b=2` in prose is not treated as logfmt.
var logfmtKVPattern = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)=("(?:[^"\\]|\\.)*"|\S+)`)

// minLogfmtKVPairs is the minimum distinct key=value pairs required before a
// line is even considered for logfmt classification.
const minLogfmtKVPairs = 2

// minLogfmtDominance is the minimum fraction of non-whitespace characters that
// must be covered by key=value matches for a line to count as logfmt.
const minLogfmtDominance = 0.5

// parseLogfmtKV returns the distinct keys present as key=value pairs in line.
func parseLogfmtKV(line string) []string {
	matches := logfmtKVPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		key := m[1]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

// isLogfmtLine reports whether line looks like real logfmt rather than prose
// that happens to contain a couple of key=value fragments. Requires at least
// minLogfmtKVPairs distinct keys whose matches cover >= minLogfmtDominance of
// the line's non-whitespace content.
func isLogfmtLine(line string) bool {
	keys := parseLogfmtKV(line)
	if len(keys) < minLogfmtKVPairs {
		return false
	}
	spans := logfmtKVPattern.FindAllStringIndex(line, -1)
	matchedLen := 0
	for _, span := range spans {
		matchedLen += span[1] - span[0]
	}
	nonSpace := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ', '\t', '\n', '\r':
		default:
			nonSpace++
		}
	}
	if nonSpace == 0 {
		return false
	}
	return float64(matchedLen)/float64(nonSpace) >= minLogfmtDominance
}

// Well-known plaintext severity/level patterns, tried in priority order.
// Both are case-insensitive. levelTimestampPattern anchors on a preceding
// HH:MM:SS(.mmm) timestamp (common in Java/Log4j-style lines).
// levelBracketOrStartPattern is the conservative fallback: severity only at
// line-start (optional leading whitespace) or inside []. A bare
// word-boundary match anywhere in the line is intentionally NOT used — it
// fabricates level from URL path segments (`/v1/INFO`) and prose
// (`no ERROR occurred`).
const (
	levelTimestampPattern      = `(?i)\d{2}:\d{2}:\d{2}[.,]\d+\s+(?P<level>TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\b`
	levelBracketOrStartPattern = `(?i)(?:^\s*|\[)(?P<level>TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)(?:\]|\b)`
)

var (
	levelTimestampRegexp      = regexp.MustCompile(levelTimestampPattern)
	levelBracketOrStartRegexp = regexp.MustCompile(levelBracketOrStartPattern)
)

// severityFamilyNames are indexed / body-derived names that all represent the
// same semantic "how severe is this log" signal. Body-derived level/severity
// is suppressed when any of these is already indexed (see merge below).
var severityFamilyNames = map[string]struct{}{
	"severity":     {},
	"severitytext": {},
	"level":        {},
}

func isSeverityFamilyName(name string) bool {
	_, ok := severityFamilyNames[strings.ToLower(name)]
	return ok
}

func hasIndexedSeverityFamily(indexedNames []string) bool {
	for _, name := range indexedNames {
		if isSeverityFamilyName(name) {
			return true
		}
	}
	return false
}

// sampleBodyDerivedAttributes fetches up to bodySampleLimit raw rows for the
// pipeline and derives attributes from the top-level keys of rows whose Body is
// a JSON object. Any failure degrades to nil — Body discovery is best-effort
// and never blocks the indexed-attribute response (the call is also bounded by
// PerChunkHTTPTimeout so a slow raw-log scan cannot stall discovery).
func sampleBodyDerivedAttributes(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, startSec, endSec int64, index string) []LogAttribute {
	ctx, cancel := context.WithTimeout(ctx, constants.PerChunkHTTPTimeout)
	defer cancel()

	result, err := executeLogJSONQuery(ctx, client, cfg, pipeline, startSec*1000, endSec*1000, bodySampleLimit, index)
	if err != nil {
		return nil
	}
	lines := extractSampleBodyLines(result)
	if len(lines) == 0 {
		return nil
	}

	freq := map[string]int{}       // JSON body-derived key frequency
	logfmtFreq := map[string]int{} // logfmt body-derived key frequency
	var levelTSCount, levelBracketCount int

	// Cascade is result-level, not per-line: the first format that produces
	// any keys for the sample wins (JSON → logfmt → plaintext level). One
	// stray JSON/logfmt line in an otherwise-plaintext sample therefore
	// suppresses downstream detection for the whole sample. Acceptable for
	// a 5-line best-effort probe; callers that need mixed-format discovery
	// should scope the pipeline tighter.
	for _, line := range lines {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			for key := range obj {
				if !safeBodyKeyPattern.MatchString(key) {
					continue
				}
				freq[key]++
			}
			continue
		}

		// Not JSON: try logfmt (key=value pairs that dominate the line).
		if isLogfmtLine(line) {
			for _, key := range parseLogfmtKV(line) {
				if !safeBodyKeyPattern.MatchString(key) {
					continue
				}
				logfmtFreq[key]++
			}
			continue
		}

		// Not logfmt either: fall back to a well-known plaintext-inline
		// severity token, preferring a timestamp-anchored match.
		if levelTimestampRegexp.MatchString(line) {
			levelTSCount++
		} else if levelBracketOrStartRegexp.MatchString(line) {
			levelBracketCount++
		}
	}

	// JSON path (unchanged, first priority): the original body-derived
	// discovery for structured JSON bodies.
	if len(freq) > 0 {
		keys := make([]string, 0, len(freq))
		for key := range freq {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if freq[keys[i]] != freq[keys[j]] {
				return freq[keys[i]] > freq[keys[j]]
			}
			return keys[i] < keys[j]
		})
		if len(keys) > maxBodyDerivedKeys {
			keys = keys[:maxBodyDerivedKeys]
		}

		out := make([]LogAttribute, 0, len(keys))
		for _, key := range keys {
			out = append(out, LogAttribute{
				Name:           key,
				FilterField:    fmt.Sprintf("attributes['%s']", key),
				Hint:           bodyDerivedHint(key),
				Source:         "body",
				SampleCoverage: fmt.Sprintf("%d/%d", freq[key], len(lines)),
			})
		}
		return out
	}

	// logfmt fallback: no line was JSON, but at least one line matched
	// logfmt key=value pairs.
	if len(logfmtFreq) > 0 {
		keys := make([]string, 0, len(logfmtFreq))
		for key := range logfmtFreq {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if logfmtFreq[keys[i]] != logfmtFreq[keys[j]] {
				return logfmtFreq[keys[i]] > logfmtFreq[keys[j]]
			}
			return keys[i] < keys[j]
		})
		if len(keys) > maxBodyDerivedKeys {
			keys = keys[:maxBodyDerivedKeys]
		}

		out := make([]LogAttribute, 0, len(keys))
		for _, key := range keys {
			out = append(out, LogAttribute{
				Name:           key,
				FilterField:    fmt.Sprintf("attributes['%s']", key),
				Hint:           logfmtBodyDerivedHint(key),
				Source:         "body",
				SampleCoverage: fmt.Sprintf("%d/%d", logfmtFreq[key], len(lines)),
			})
		}
		return out
	}

	// plaintext-inline fallback: neither JSON nor logfmt, but a severity
	// token was found in at least one sampled line. Conservative — only
	// surface "level" when the pattern actually matched a sampled line.
	if levelTSCount > 0 || levelBracketCount > 0 {
		pattern, count := levelBracketOrStartPattern, levelBracketCount
		if levelTSCount > 0 {
			pattern, count = levelTimestampPattern, levelTSCount
		}
		return []LogAttribute{{
			Name:           "level",
			FilterField:    "attributes['level']",
			Hint:           regexpBodyDerivedHint(pattern, "level", "<value>"),
			Source:         "body",
			SampleCoverage: fmt.Sprintf("%d/%d", count, len(lines)),
		}}
	}

	return nil
}

// extractSampleBodyLines pulls the raw Body line of each sampled log entry from
// a query_range streams response (data.result[].values[][1]). It reuses
// extractResultItems so both readers of the query_range shape stay in sync.
func extractSampleBodyLines(result map[string]interface{}) []string {
	_, items, err := extractResultItems(result)
	if err != nil {
		return nil
	}
	var lines []string
	for _, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		values, ok := entry["values"].([]interface{})
		if !ok {
			continue
		}
		for _, pair := range values {
			tuple, ok := pair.([]interface{})
			if !ok || len(tuple) < 2 {
				continue
			}
			if line, ok := tuple[1].(string); ok && line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// logFieldFilterField maps a raw log field name to the exact filter_field string
// used in a logjson condition:
//   - service    -> ServiceName
//   - severity   -> SeverityText
//   - body       -> Body
//   - resource_x -> resources['x']
//   - default    -> attributes['<name>']
//
// The series endpoint returns log attributes bare (only resource attributes are
// prefixed, with resource_), so a field name keeps its full name: e.g. a real
// attribute named log_level maps to attributes['log_level'], not attributes['level'].
func logFieldFilterField(name string) string {
	switch name {
	case "service":
		return "ServiceName"
	case "severity":
		return "SeverityText"
	case "body":
		return "Body"
	}
	if rest, ok := strings.CutPrefix(name, "resource_"); ok {
		return fmt.Sprintf("resources['%s']", rest)
	}
	return fmt.Sprintf("attributes['%s']", name)
}

// fetchLogSeriesFieldNames POSTs the given pipeline to /logs/api/v2/series/json
// and returns the union of field names present across all returned label-sets.
func fetchLogSeriesFieldNames(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, queryParams url.Values) ([]string, error) {
	apiURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointLogsSeries, queryParams.Encode())

	requestBody := map[string]interface{}{
		"pipeline": pipeline,
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
	httpReq.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errorBody)
		return nil, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errorBody)
	}

	var result logSeriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response for series api: %+v", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API returned non-success status: %s", result.Status)
	}

	// Union the keys across every label-set so a field present in any matching
	// log is reported.
	seen := map[string]struct{}{}
	for _, entry := range result.Data {
		for fieldName := range entry {
			if fieldName == "" {
				continue
			}
			seen[fieldName] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for fieldName := range seen {
		names = append(names, fieldName)
	}
	sort.Strings(names)
	return names, nil
}

// NewGetLogAttributesForPipelineHandler creates a handler that returns the log
// attributes present for a given pipeline, each enriched with its filter_field.
func NewGetLogAttributesForPipelineHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogAttributesForPipelineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogAttributesForPipelineArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Pipeline) == 0 {
			return nil, nil, fmt.Errorf("pipeline parameter is required. Provide at least one filter stage to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}]")
		}

		params := make(map[string]interface{})
		if args.LookbackMinutes > 0 {
			params["lookback_minutes"] = args.LookbackMinutes
		}
		if args.StartTimeISO != "" {
			params["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			params["end_time_iso"] = args.EndTimeISO
		}

		const defaultLogAttributesLookback = 15
		startTimeParsed, endTimeParsed, err := utils.GetTimeRange(params, defaultLogAttributesLookback)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse time range: %w", err)
		}
		endTime := endTimeParsed.Unix()
		startTime := startTimeParsed.Unix()
		// Cap the window magnitude to keep server cost bounded, matching get_log_attributes.
		maxWindowSeconds := int64(utils.MaxLogAttributeLookbackMinutes * 60)
		if endTime-startTime > maxWindowSeconds {
			startTime = endTime - maxWindowSeconds
		}

		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		normalizedIndex, err := utils.NormalizeLogIndex(args.Index)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid index: %w", err)
		}

		queryParams := url.Values{}
		queryParams.Set("region", region)
		queryParams.Set("start", fmt.Sprintf("%d", startTime))
		queryParams.Set("end", fmt.Sprintf("%d", endTime))
		if normalizedIndex != "" {
			queryParams.Set("index", normalizedIndex)
		}

		// Best-effort, in parallel with the series fetch: fields that exist only
		// inside the log Body as JSON — they need a parse stage (carried in the
		// hint) before use. The sampling honors the same region override.
		samplingCfg := cfg
		samplingCfg.Region = region
		bodyCh := make(chan []LogAttribute, 1)
		go func() {
			bodyCh <- sampleBodyDerivedAttributes(ctx, client, samplingCfg, args.Pipeline, startTime, endTime, normalizedIndex)
		}()

		names, err := fetchLogSeriesFieldNames(ctx, client, cfg, args.Pipeline, queryParams)
		if err != nil {
			return nil, nil, err
		}

		out := make([]LogAttribute, 0, len(names))
		indexedFilterFields := make(map[string]struct{}, len(names))
		indexedHasSeverity := hasIndexedSeverityFamily(names)
		for _, name := range names {
			filterField := logFieldFilterField(name)
			indexedFilterFields[filterField] = struct{}{}
			out = append(out, LogAttribute{
				Name:        name,
				FilterField: filterField,
				Hint:        fmt.Sprintf("{\"$eq\":[\"%s\",\"<value>\"]}", filterField),
			})
		}

		// Merge: drop a body-derived entry when (a) an indexed entry already
		// exposes the SAME filter_field (true duplicate), or (b) it is a
		// severity-family name (level/severity/SeverityText) and any indexed
		// severity-family field is already present. Indexed severity is the
		// directly-filterable signal the product UI renders; a parallel
		// body-derived level that requires a regexp parse is inferior and
		// must not be advertised alongside it.
		for _, attr := range <-bodyCh {
			if _, dup := indexedFilterFields[attr.FilterField]; dup {
				continue
			}
			if indexedHasSeverity && isSeverityFamilyName(attr.Name) {
				continue
			}
			out = append(out, attr)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

		payload, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal attributes: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(payload),
				},
			},
		}, nil, nil
	}
}
