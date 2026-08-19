package traces

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	traceEvidenceSanitizerVersion      = "trace-evidence-sanitizer/v1"
	traceEvidenceMaxStringBytes        = 512
	traceEvidenceMaxCollectionItems    = 64
	traceEvidenceMaxDepth              = 12
	traceEvidenceMaxSerializedBytes    = 512 * 1024
	traceEvidenceRedactedValue         = "[REDACTED]"
	traceEvidenceTruncatedStringSuffix = "...[truncated]"
	traceEvidenceSpanTruncationWarning = "span list truncated to satisfy trace-evidence-sanitizer/v1 serialized-byte limit"
)

var (
	evidenceEmailPattern   = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`)
	evidenceIPv4Pattern    = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	evidenceIPTokenPattern = regexp.MustCompile(`[0-9A-Fa-f:.%]+`)
	evidenceAuthPattern    = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/-]+=*`)
)

type EvidenceSanitizationAction struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type EvidenceSanitizationLimits struct {
	MaxStringBytes     int `json:"max_string_bytes"`
	MaxCollectionItems int `json:"max_collection_items"`
	MaxDepth           int `json:"max_depth"`
	MaxSerializedBytes int `json:"max_serialized_bytes"`
}

// EvidenceSanitization makes every lossy safety transformation machine-readable.
// Counts deliberately omit paths: a path can itself carry identifiers or PII.
type EvidenceSanitization struct {
	Version     string                       `json:"version"`
	Redacted    bool                         `json:"redacted"`
	Truncated   bool                         `json:"truncated"`
	Redactions  []EvidenceSanitizationAction `json:"redactions"`
	Truncations []EvidenceSanitizationAction `json:"truncations"`
	Limits      EvidenceSanitizationLimits   `json:"limits"`
}

type traceEvidenceSanitizer struct {
	redactions  map[string]int
	truncations map[string]int
}

func newTraceEvidenceSanitizer() *traceEvidenceSanitizer {
	return &traceEvidenceSanitizer{
		redactions:  make(map[string]int),
		truncations: make(map[string]int),
	}
}

func (s *traceEvidenceSanitizer) metadata() EvidenceSanitization {
	redactions := sanitizationActions(s.redactions)
	truncations := sanitizationActions(s.truncations)
	return EvidenceSanitization{
		Version:     traceEvidenceSanitizerVersion,
		Redacted:    len(redactions) > 0,
		Truncated:   len(truncations) > 0,
		Redactions:  redactions,
		Truncations: truncations,
		Limits: EvidenceSanitizationLimits{
			MaxStringBytes:     traceEvidenceMaxStringBytes,
			MaxCollectionItems: traceEvidenceMaxCollectionItems,
			MaxDepth:           traceEvidenceMaxDepth,
			MaxSerializedBytes: traceEvidenceMaxSerializedBytes,
		},
	}
}

func sanitizationActions(counts map[string]int) []EvidenceSanitizationAction {
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	actions := make([]EvidenceSanitizationAction, 0, len(kinds))
	for _, kind := range kinds {
		actions = append(actions, EvidenceSanitizationAction{Kind: kind, Count: counts[kind]})
	}
	return actions
}

// marshalSanitizedTraceWaterfall is the only serialization path for waterfall
// evidence. The returned bytes are safe to use for both MCP text and structured
// content without a second marshal.
func marshalSanitizedTraceWaterfall(resp TraceWaterfallResponse) ([]byte, error) {
	sanitizer := newTraceEvidenceSanitizer()
	sanitizer.sanitizeResponse(&resp)
	resp.Evidence.Sanitization = sanitizer.metadata()
	if resp.Evidence.Sanitization.Truncated {
		applySanitizerTruncation(&resp)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("serialize sanitized trace waterfall: %w", err)
	}
	if len(b) <= traceEvidenceMaxSerializedBytes {
		return b, nil
	}

	// Selected details are optional evidence. Drop them as one deterministic unit
	// before failing the whole result, and disclose that serialized-byte truncation.
	if selected := resp.Data.SelectedSpan; selected != nil {
		resp.Data.SelectedSpan = &WaterfallSelectedSpan{SpanID: selected.SpanID}
		sanitizer.truncations["serialized_bytes"]++
		resp.Evidence.Sanitization = sanitizer.metadata()
		applySanitizerTruncation(&resp)
		b, err = json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("serialize bounded trace waterfall: %w", err)
		}
		if len(b) <= traceEvidenceMaxSerializedBytes {
			return b, nil
		}
	}
	if len(resp.Data.Spans) > 0 {
		if bounded, ok := fitTraceWaterfallSpanPrefix(resp, sanitizer); ok {
			return bounded, nil
		}
	}
	return nil, fmt.Errorf("sanitized trace waterfall exceeds %d-byte evidence limit", traceEvidenceMaxSerializedBytes)
}

func fitTraceWaterfallSpanPrefix(resp TraceWaterfallResponse, sanitizer *traceEvidenceSanitizer) ([]byte, bool) {
	originalSpans := resp.Data.Spans
	low, high := 0, len(originalSpans)
	var best []byte
	for low <= high {
		mid := low + (high-low)/2
		candidate := resp
		candidate.Data.Spans = append([]WaterfallSpan(nil), originalSpans[:mid]...)
		candidate.Evidence.ReturnedSpans = mid
		recountWaterfallSummary(&candidate.Data.Summary, candidate.Data.Spans)
		candidate.Data.SlowestSpans = filterWaterfallSpans(candidate.Data.SlowestSpans, candidate.Data.Spans)
		candidate.Data.LargestSelfTimeContributors = filterWaterfallSpans(candidate.Data.LargestSelfTimeContributors, candidate.Data.Spans)
		candidate.Data.Summary.RootSpanIDs = waterfallRootSpanIDs(candidate.Data.Spans)
		if candidate.Data.SelectedSpan != nil && !waterfallContainsSpan(candidate.Data.Spans, candidate.Data.SelectedSpan.SpanID) {
			candidate.Data.SelectedSpan = nil
		}
		sanitizer.truncations["serialized_spans"] = len(originalSpans) - mid
		candidate.Evidence.Sanitization = sanitizer.metadata()
		applySanitizerTruncation(&candidate)
		candidate.Evidence.Partial = true
		candidate.Evidence.Warnings = appendUniqueSorted(candidate.Evidence.Warnings, traceEvidenceSpanTruncationWarning)
		body, err := json.Marshal(candidate)
		if err != nil || len(body) > traceEvidenceMaxSerializedBytes {
			high = mid - 1
			continue
		}
		best = body
		low = mid + 1
	}
	delete(sanitizer.truncations, "serialized_spans")
	return best, best != nil
}

func recountWaterfallSummary(summary *WaterfallSummary, spans []WaterfallSpan) {
	services := make(map[string]struct{})
	summary.SpanCount = len(spans)
	summary.ErrorCount = 0
	summary.MaxDepth = 0
	for _, span := range spans {
		if span.Service != "" {
			services[span.Service] = struct{}{}
		}
		if span.Status == traceWaterfallErrorStatusCode {
			summary.ErrorCount++
		}
		if span.Depth > summary.MaxDepth {
			summary.MaxDepth = span.Depth
		}
	}
	summary.ServiceCount = len(services)
}

func filterWaterfallSpans(values, retained []WaterfallSpan) []WaterfallSpan {
	ids := make(map[string]struct{}, len(retained))
	for _, span := range retained {
		ids[span.SpanID] = struct{}{}
	}
	out := make([]WaterfallSpan, 0, len(values))
	for _, span := range values {
		if _, ok := ids[span.SpanID]; ok {
			out = append(out, span)
		}
	}
	return out
}

func waterfallContainsSpan(spans []WaterfallSpan, spanID string) bool {
	for _, span := range spans {
		if span.SpanID == spanID {
			return true
		}
	}
	return false
}

func waterfallRootSpanIDs(spans []WaterfallSpan) []string {
	ids := make(map[string]struct{}, len(spans))
	for _, span := range spans {
		ids[span.SpanID] = struct{}{}
	}
	roots := make([]string, 0)
	for _, span := range spans {
		if span.ParentSpanID == "" {
			roots = append(roots, span.SpanID)
			continue
		}
		if _, ok := ids[span.ParentSpanID]; !ok {
			roots = append(roots, span.SpanID)
		}
	}
	sort.Strings(roots)
	return roots
}

func appendUniqueSorted(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func (s *traceEvidenceSanitizer) sanitizeResponse(resp *TraceWaterfallResponse) {
	resp.Request.Scope.TraceID = s.sanitizeString("trace_id", resp.Request.Scope.TraceID)
	resp.Request.Scope.Environment = s.sanitizeString("environment", resp.Request.Scope.Environment)
	resp.Request.TraceID = s.sanitizeString("trace_id", resp.Request.TraceID)
	resp.Request.SelectedSpanID = s.sanitizeString("span_id", resp.Request.SelectedSpanID)
	resp.Evidence.Warnings = s.sanitizeStrings("warning", resp.Evidence.Warnings)
	if resp.Evidence.Provenance.QueryID != nil {
		queryID := s.sanitizeString("query_id", *resp.Evidence.Provenance.QueryID)
		resp.Evidence.Provenance.QueryID = &queryID
	}
	resp.Interpretation.Summary = s.sanitizeString("summary", resp.Interpretation.Summary)
	resp.Interpretation.Limitations = s.sanitizeStrings("limitation", resp.Interpretation.Limitations)
	resp.Data.Summary.RootSpanIDs = s.sanitizeStrings("span_id", resp.Data.Summary.RootSpanIDs)
	resp.Data.Spans = s.sanitizeWaterfallSpans(resp.Data.Spans)
	resp.Data.SlowestSpans = s.sanitizeWaterfallSpans(resp.Data.SlowestSpans)
	resp.Data.LargestSelfTimeContributors = s.sanitizeWaterfallSpans(resp.Data.LargestSelfTimeContributors)
	if resp.Data.SelectedSpan != nil {
		resp.Data.SelectedSpan = s.sanitizeSelectedSpan(resp.Data.SelectedSpan)
	}
}

func (s *traceEvidenceSanitizer) sanitizeStrings(key string, values []string) []string {
	if values == nil {
		return nil
	}
	limit := len(values)
	if limit > traceEvidenceMaxCollectionItems {
		s.truncations["collection_items"] += limit - traceEvidenceMaxCollectionItems
		limit = traceEvidenceMaxCollectionItems
	}
	out := make([]string, 0, limit)
	for _, value := range values[:limit] {
		out = append(out, s.sanitizeString(key, value))
	}
	return out
}

func (s *traceEvidenceSanitizer) sanitizeWaterfallSpans(spans []WaterfallSpan) []WaterfallSpan {
	if spans == nil {
		return nil
	}
	limit := len(spans)
	if limit > traceWaterfallMaxSpansCeiling {
		s.truncations["collection_items"] += limit - traceWaterfallMaxSpansCeiling
		limit = traceWaterfallMaxSpansCeiling
	}
	out := append([]WaterfallSpan(nil), spans[:limit]...)
	for i := range out {
		out[i].SpanID = s.sanitizeString("span_id", out[i].SpanID)
		out[i].ParentSpanID = s.sanitizeString("parent_span_id", out[i].ParentSpanID)
		out[i].Service = s.sanitizeString("service", out[i].Service)
		out[i].Operation = s.sanitizeString("operation", out[i].Operation)
		out[i].Kind = s.sanitizeString("kind", out[i].Kind)
		out[i].Status = s.sanitizeString("status", out[i].Status)
	}
	return out
}

func applySanitizerTruncation(resp *TraceWaterfallResponse) {
	resp.Evidence.Truncated = true
	if resp.Interpretation.EvidenceQuality == evidenceQualityHigh {
		resp.Interpretation.EvidenceQuality = evidenceQualityMedium
	}
	const limitation = "Trace evidence was truncated by trace-evidence-sanitizer/v1."
	for _, existing := range resp.Interpretation.Limitations {
		if existing == limitation {
			return
		}
	}
	resp.Interpretation.Limitations = append(resp.Interpretation.Limitations, limitation)
}

func newTraceWaterfallToolResult(b []byte, meta mcp.Meta) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Meta:              meta,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(b)}},
		StructuredContent: json.RawMessage(b),
	}
}

func (s *traceEvidenceSanitizer) sanitizeSelectedSpan(selected *WaterfallSelectedSpan) *WaterfallSelectedSpan {
	out := &WaterfallSelectedSpan{SpanID: s.sanitizeString("span_id", selected.SpanID)}
	if selected.ResourceAttributes != nil {
		out.ResourceAttributes = make(map[string]string)
		keys := sortedStringKeys(selected.ResourceAttributes)
		keys = s.capKeys(keys)
		for _, key := range keys {
			safeKey := s.sanitizeMapKey(key)
			if _, exists := out.ResourceAttributes[safeKey]; exists {
				s.truncations["key_collision"]++
				continue
			}
			if kind := redactionKindForKey(key); kind != "" {
				s.redactions[kind]++
				out.ResourceAttributes[safeKey] = traceEvidenceRedactedValue
				continue
			}
			out.ResourceAttributes[safeKey] = s.sanitizeString(key, selected.ResourceAttributes[key])
		}
	}
	if selected.SpanAttributes != nil {
		out.SpanAttributes = s.sanitizeMap(selected.SpanAttributes, 0)
	}
	if selected.Events != nil {
		out.Events = s.sanitizeMapList(selected.Events, 0)
	}
	if selected.Links != nil {
		out.Links = s.sanitizeMapList(selected.Links, 0)
	}
	return out
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *traceEvidenceSanitizer) capKeys(keys []string) []string {
	if len(keys) <= traceEvidenceMaxCollectionItems {
		return keys
	}
	s.truncations["collection_items"] += len(keys) - traceEvidenceMaxCollectionItems
	return keys[:traceEvidenceMaxCollectionItems]
}

func (s *traceEvidenceSanitizer) sanitizeMapList(values []map[string]any, depth int) []map[string]any {
	limit := len(values)
	if limit > traceEvidenceMaxCollectionItems {
		s.truncations["collection_items"] += limit - traceEvidenceMaxCollectionItems
		limit = traceEvidenceMaxCollectionItems
	}
	out := make([]map[string]any, 0, limit)
	for _, value := range values[:limit] {
		out = append(out, s.sanitizeMap(value, depth+1))
	}
	return out
}

func (s *traceEvidenceSanitizer) sanitizeMap(values map[string]any, depth int) map[string]any {
	if depth >= traceEvidenceMaxDepth {
		s.truncations["nesting_depth"]++
		return map[string]any{"_truncated": true}
	}
	out := make(map[string]any)
	keys := s.capKeys(sortedStringKeys(values))
	for _, key := range keys {
		safeKey := s.sanitizeMapKey(key)
		if _, exists := out[safeKey]; exists {
			s.truncations["key_collision"]++
			continue
		}
		if kind := redactionKindForKey(key); kind != "" {
			s.redactions[kind]++
			out[safeKey] = traceEvidenceRedactedValue
			continue
		}
		out[safeKey] = s.sanitizeAny(key, values[key], depth+1)
	}
	return out
}

func (s *traceEvidenceSanitizer) sanitizeMapKey(key string) string {
	return s.sanitizeString("attribute_key", key)
}

func (s *traceEvidenceSanitizer) sanitizeAny(key string, value any, depth int) any {
	if depth >= traceEvidenceMaxDepth {
		s.truncations["nesting_depth"]++
		return "[TRUNCATED_DEPTH]"
	}
	switch value := value.(type) {
	case string:
		return s.sanitizeString(key, value)
	case map[string]any:
		return s.sanitizeMap(value, depth)
	case map[string]string:
		converted := make(map[string]any, len(value))
		for k, v := range value {
			converted[k] = v
		}
		return s.sanitizeMap(converted, depth)
	case []any:
		limit := len(value)
		if limit > traceEvidenceMaxCollectionItems {
			s.truncations["collection_items"] += limit - traceEvidenceMaxCollectionItems
			limit = traceEvidenceMaxCollectionItems
		}
		out := make([]any, 0, limit)
		for _, item := range value[:limit] {
			out = append(out, s.sanitizeAny(key, item, depth+1))
		}
		return out
	case []map[string]any:
		return s.sanitizeMapList(value, depth)
	default:
		return value
	}
}

func redactionKindForKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	canonical := strings.NewReplacer(".", "_", "-", "_").Replace(normalized)
	for _, token := range []string{"statement", "query", "query.text", "sql"} {
		canonicalToken := strings.ReplaceAll(token, ".", "_")
		if canonical == canonicalToken || strings.HasSuffix(canonical, "_"+canonicalToken) {
			return "query_literal"
		}
	}
	for _, token := range []string{"authorization", "auth", "cookie", "password", "passwd", "secret", "client_secret", "token", "api_key", "apikey", "access_key", "private_key", "credential", "connection_string"} {
		if canonical == token || strings.HasSuffix(canonical, "_"+token) {
			return "credential"
		}
	}
	for _, token := range []string{"email", "phone", "user.name", "user.id", "enduser.id", "client.ip", "client.address", "ip.address", "source.ip", "destination.ip"} {
		canonicalToken := strings.ReplaceAll(token, ".", "_")
		if canonical == canonicalToken || strings.HasSuffix(canonical, "_"+canonicalToken) {
			return "pii"
		}
	}
	return ""
}

func (s *traceEvidenceSanitizer) sanitizeString(key, value string) string {
	if evidenceAuthPattern.MatchString(value) {
		s.redactions["credential"]++
		return traceEvidenceRedactedValue
	}
	value = s.sanitizeURL(key, value)
	if evidenceEmailPattern.MatchString(value) {
		value = evidenceEmailPattern.ReplaceAllString(value, "[REDACTED_EMAIL]")
		s.redactions["pii"]++
	}
	if evidenceIPv4Pattern.MatchString(value) {
		value = evidenceIPv4Pattern.ReplaceAllString(value, "[REDACTED_IP]")
		s.redactions["pii"]++
	}
	if net.ParseIP(strings.TrimSpace(value)) != nil {
		value = "[REDACTED_IP]"
		s.redactions["pii"]++
	}
	value = evidenceIPTokenPattern.ReplaceAllStringFunc(value, func(candidate string) string {
		trimmed := strings.Trim(candidate, ".")
		withoutZone := strings.TrimSuffix(trimmed, "%")
		if zone := strings.LastIndexByte(withoutZone, '%'); zone >= 0 {
			withoutZone = withoutZone[:zone]
		}
		if strings.Count(withoutZone, ":") >= 2 && net.ParseIP(withoutZone) != nil {
			s.redactions["pii"]++
			return "[REDACTED_IP]"
		}
		return candidate
	})
	return s.truncateString(value)
}

func (s *traceEvidenceSanitizer) sanitizeURL(key, value string) string {
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			if parsed.User != nil {
				parsed.User = nil
				s.redactions["credential"]++
			}
			if parsed.RawQuery != "" || parsed.Fragment != "" {
				parsed.RawQuery = "redacted"
				parsed.Fragment = ""
				s.redactions["url_query"]++
			}
			host := parsed.Hostname()
			withoutZone := host
			if zone := strings.LastIndexByte(withoutZone, '%'); zone >= 0 {
				withoutZone = withoutZone[:zone]
			}
			if net.ParseIP(withoutZone) != nil {
				port := parsed.Port()
				parsed.Host = "redacted.invalid"
				if port != "" {
					parsed.Host = net.JoinHostPort(parsed.Host, port)
				}
				s.redactions["pii"]++
			}
			return parsed.String()
		}
	}
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "url") || strings.Contains(lowerKey, "target") || strings.Contains(lowerKey, "query") {
		if query := strings.IndexByte(value, '?'); query >= 0 {
			s.redactions["url_query"]++
			return value[:query] + "?[REDACTED]"
		}
	}
	return value
}

func (s *traceEvidenceSanitizer) truncateString(value string) string {
	if len(value) <= traceEvidenceMaxStringBytes {
		return value
	}
	s.truncations["string_bytes"]++
	limit := traceEvidenceMaxStringBytes - len(traceEvidenceTruncatedStringSuffix)
	if limit < 0 {
		limit = 0
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + traceEvidenceTruncatedStringSuffix
}
