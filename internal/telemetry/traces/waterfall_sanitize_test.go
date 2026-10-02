package traces

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func sanitizerTestResponse() TraceWaterfallResponse {
	deep := map[string]any{"leaf": "safe"}
	for i := 0; i < traceEvidenceMaxDepth+2; i++ {
		deep = map[string]any{"next": deep}
	}
	resp := buildTraceWaterfall(waterfallTestInput([]TraceDetailsSpan{{
		TraceID:     "t",
		SpanID:      "selected",
		ServiceName: "owner@example.com",
		SpanName:    "Bearer operation-secret",
		Timestamp:   "2026-07-15T00:00:00Z",
		Duration:    10_000_000,
		ResourceAttributes: map[string]string{
			"password":          "resource-secret",
			"api.key":           "api-key-secret",
			"owner@example.com": "safe",
		},
		SpanAttributes: map[string]any{
			"authorization":  "Bearer top-secret-token",
			"customer_email": "alice@example.com",
			"message_auth":   "Basic dXNlcjpwYXNz",
			"message_email":  "contact dave@example.com",
			"message_ip":     "peer 198.51.100.7",
			"message_ipv6":   "peer [2001:db8::2]",
			"http.url":       "https://user:password@example.com/orders?token=query-secret&email=alice@example.com",
			"peer.url":       "https://[2001:db8::3]:8443/orders",
			"db.statement":   "SELECT * FROM users WHERE email = 'carol@example.com'",
			"deep":           deep,
			"nested": map[string]any{
				"password": "nested-secret",
				"long":     strings.Repeat("界", traceEvidenceMaxStringBytes),
				"items":    make([]any, traceEvidenceMaxCollectionItems+3),
			},
		},
		Events: []map[string]any{{"name": "ok", "client.ip": "203.0.113.4"}, {"peer": "connected to 2001:db8::1"}},
		Links:  []map[string]any{{"trace": "safe", "email": "bob@example.com"}},
	}}, 500, "selected"))
	return resp
}

func TestTraceWaterfallSanitizerIsDeterministicAndReportsTypedActions(t *testing.T) {
	resp := sanitizerTestResponse()
	first, err := marshalSanitizedTraceWaterfall(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) > traceEvidenceMaxSerializedBytes {
		t.Fatalf("serialized bytes=%d, cap=%d", len(first), traceEvidenceMaxSerializedBytes)
	}
	for _, secret := range []string{"operation-secret", "top-secret-token", "resource-secret", "api-key-secret", "nested-secret", "query-secret", "SELECT * FROM users", "alice@example.com", "bob@example.com", "owner@example.com", "dave@example.com", "198.51.100.7", "203.0.113.4", "2001:db8::1", "2001:db8::2", "2001:db8::3", "user:password"} {
		if strings.Contains(string(first), secret) {
			t.Fatalf("sanitized evidence leaked %q: %s", secret, first)
		}
	}
	for _, marker := range []string{traceEvidenceSanitizerVersion, `"redactions"`, `"truncations"`, `"credential"`, `"pii"`, `"url_query"`, `"query_literal"`, `"string_bytes"`, `"collection_items"`, `"nesting_depth"`, `"max_depth"`} {
		if !strings.Contains(string(first), marker) {
			t.Fatalf("sanitized evidence missing typed marker %q: %s", marker, first)
		}
	}
	var envelope struct {
		Evidence       WaterfallEvidence       `json:"evidence"`
		Interpretation WaterfallInterpretation `json:"interpretation"`
	}
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Evidence.Truncated || !envelope.Evidence.Sanitization.Truncated {
		t.Fatalf("sanitizer truncation must set both evidence flags: %+v", envelope.Evidence)
	}
	if envelope.Interpretation.EvidenceQuality == evidenceQualityHigh {
		t.Fatalf("truncated selected-span evidence cannot remain high quality: %+v", envelope.Interpretation)
	}
	selected := sanitizerSelectedSpan(t, first)
	if selected.ResourceAttributes["[REDACTED_EMAIL]"] != "safe" {
		t.Fatalf("sensitive attribute key was not sanitized: %+v", selected.ResourceAttributes)
	}
	for _, key := range []string{"message_auth", "message_email", "message_ip", "message_ipv6"} {
		value, _ := selected.SpanAttributes[key].(string)
		if value == "" || strings.ContainsAny(value, "@:") && !strings.Contains(value, "[REDACTED") {
			t.Fatalf("neutral-key value %q was not redacted: %q", key, value)
		}
	}
	if peerURL, _ := selected.SpanAttributes["peer.url"].(string); !strings.Contains(peerURL, "redacted.invalid:8443") {
		t.Fatalf("IPv6 URL host was not redacted: %q", peerURL)
	}
	long, ok := selected.SpanAttributes["nested"].(map[string]any)["long"].(string)
	if !ok || len(long) != traceEvidenceMaxStringBytes || !strings.HasSuffix(long, traceEvidenceTruncatedStringSuffix) {
		t.Fatalf("long UTF-8 value was not bounded to %d bytes: %q", traceEvidenceMaxStringBytes, long)
	}
	items, ok := selected.SpanAttributes["nested"].(map[string]any)["items"].([]any)
	if !ok || len(items) != traceEvidenceMaxCollectionItems {
		t.Fatalf("collection length=%d, want %d", len(items), traceEvidenceMaxCollectionItems)
	}
	if count := sanitizationActionCount(envelope.Evidence.Sanitization.Truncations, "collection_items"); count != 3 {
		t.Fatalf("collection truncation count=%d, want 3", count)
	}
	if count := sanitizationActionCount(envelope.Evidence.Sanitization.Truncations, "nesting_depth"); count == 0 {
		t.Fatal("deep value did not report nesting_depth truncation")
	}
	for i := 0; i < 50; i++ {
		next, err := marshalSanitizedTraceWaterfall(resp)
		if err != nil {
			t.Fatal(err)
		}
		if string(next) != string(first) {
			t.Fatalf("sanitizer bytes changed on run %d", i)
		}
	}
}

func sanitizationActionCount(actions []EvidenceSanitizationAction, kind string) int {
	for _, action := range actions {
		if action.Kind == kind {
			return action.Count
		}
	}
	return 0
}

func sanitizerSelectedSpan(t *testing.T, body []byte) WaterfallSelectedSpan {
	t.Helper()
	var envelope struct {
		Data struct {
			SelectedSpan WaterfallSelectedSpan `json:"selected_span"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.SelectedSpan
}

func TestTraceWaterfallToolResultUsesOneCanonicalByteSlice(t *testing.T) {
	b, err := marshalSanitizedTraceWaterfall(sanitizerTestResponse())
	if err != nil {
		t.Fatal(err)
	}
	result := newTraceWaterfallToolResult(b, mcp.Meta{"reference_url": "/traces"})
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type=%T", result.Content[0])
	}
	structured, ok := result.StructuredContent.(json.RawMessage)
	if !ok {
		t.Fatalf("structured type=%T", result.StructuredContent)
	}
	if text.Text != string(b) || string(structured) != string(b) {
		t.Fatalf("text/structured result diverged from canonical bytes")
	}
}

func TestTraceEvidenceSanitizerExactBoundaries(t *testing.T) {
	s := newTraceEvidenceSanitizer()
	atStringLimit := strings.Repeat("x", traceEvidenceMaxStringBytes)
	if got := s.truncateString(atStringLimit); got != atStringLimit {
		t.Fatalf("string at limit changed: %d bytes", len(got))
	}
	if got := s.truncateString(atStringLimit + "x"); len(got) != traceEvidenceMaxStringBytes || !strings.HasSuffix(got, traceEvidenceTruncatedStringSuffix) {
		t.Fatalf("string above limit was not bounded exactly: %d bytes", len(got))
	}
	atCollectionLimit := make([]any, traceEvidenceMaxCollectionItems)
	if got := s.sanitizeAny("items", atCollectionLimit, 0).([]any); len(got) != traceEvidenceMaxCollectionItems {
		t.Fatalf("collection at limit changed: %d items", len(got))
	}
	aboveCollectionLimit := make([]any, traceEvidenceMaxCollectionItems+1)
	if got := s.sanitizeAny("items", aboveCollectionLimit, 0).([]any); len(got) != traceEvidenceMaxCollectionItems {
		t.Fatalf("collection above limit was not bounded: %d items", len(got))
	}
	if got := s.sanitizeAny("value", "safe", traceEvidenceMaxDepth-1); got != "safe" {
		t.Fatalf("value below depth limit changed: %v", got)
	}
	if got := s.sanitizeAny("value", "safe", traceEvidenceMaxDepth); got != "[TRUNCATED_DEPTH]" {
		t.Fatalf("value at depth limit was not truncated: %v", got)
	}
	if count := s.truncations["string_bytes"]; count != 1 {
		t.Fatalf("string truncation count=%d, want 1", count)
	}
	if count := s.truncations["collection_items"]; count != 1 {
		t.Fatalf("collection truncation count=%d, want 1", count)
	}
	if count := s.truncations["nesting_depth"]; count != 1 {
		t.Fatalf("depth truncation count=%d, want 1", count)
	}
}

func TestTraceWaterfallSerializationFailsClosedWhenBoundCannotFit(t *testing.T) {
	resp := buildTraceWaterfall(waterfallTestInput(nil, 500, ""))
	// Contract versions are trusted producer constants, not telemetry, so the
	// sanitizer must reject rather than rewrite a corrupted oversized value.
	resp.ContractVersion = strings.Repeat("x", traceEvidenceMaxSerializedBytes)
	if _, err := marshalSanitizedTraceWaterfall(resp); err == nil {
		t.Fatal("oversized response must fail closed")
	}
}

func TestTraceWaterfallMaximumSpanShapeKeepsCanonicalParity(t *testing.T) {
	resp := buildTraceWaterfall(waterfallTestInput(nil, traceWaterfallMaxSpansCeiling, ""))
	resp.Data.Spans = make([]WaterfallSpan, traceWaterfallMaxSpansCeiling)
	for i := range resp.Data.Spans {
		resp.Data.Spans[i] = WaterfallSpan{
			SpanID: fmt.Sprintf("%016x", i), Service: strings.Repeat("s", traceEvidenceMaxStringBytes),
			Operation: strings.Repeat("o", traceEvidenceMaxStringBytes),
		}
	}
	resp.Data.Summary.SpanCount = len(resp.Data.Spans)
	body, err := marshalSanitizedTraceWaterfall(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > traceEvidenceMaxSerializedBytes {
		t.Fatalf("maximum shape bytes=%d, cap=%d", len(body), traceEvidenceMaxSerializedBytes)
	}
	result := newTraceWaterfallToolResult(body, nil)
	text := result.Content[0].(*mcp.TextContent)
	structured := result.StructuredContent.(json.RawMessage)
	if text.Text != string(structured) || text.Text != string(body) {
		t.Fatal("maximum-shape text and structured content diverged")
	}
	var envelope struct {
		Evidence WaterfallEvidence `json:"evidence"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Evidence.ReturnedSpans >= traceWaterfallMaxSpansCeiling || !envelope.Evidence.Partial || !envelope.Evidence.Truncated {
		t.Fatalf("maximum shape did not disclose bounded span truncation: %+v", envelope.Evidence)
	}
	if count := sanitizationActionCount(envelope.Evidence.Sanitization.Truncations, "serialized_spans"); count == 0 {
		t.Fatal("maximum shape omitted serialized_spans truncation metadata")
	}
}

func TestTraceWaterfallDropsSelectedDetailsToFitAndReportsIt(t *testing.T) {
	resp := buildTraceWaterfall(waterfallTestInput(nil, 500, ""))
	largeMap := make(map[string]any, traceEvidenceMaxCollectionItems)
	for i := 0; i < traceEvidenceMaxCollectionItems; i++ {
		largeMap[fmt.Sprintf("field_%02d", i)] = strings.Repeat("x", traceEvidenceMaxStringBytes)
	}
	events := make([]map[string]any, traceEvidenceMaxCollectionItems)
	for i := range events {
		events[i] = largeMap
	}
	resp.Data.SelectedSpan = &WaterfallSelectedSpan{SpanID: "selected", Events: events}
	body, err := marshalSanitizedTraceWaterfall(resp)
	if err != nil {
		t.Fatal(err)
	}
	selected := sanitizerSelectedSpan(t, body)
	if selected.SpanID != "selected" || selected.Events != nil {
		t.Fatalf("selected details were not dropped deterministically: %+v", selected)
	}
	var envelope struct {
		Evidence WaterfallEvidence `json:"evidence"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if count := sanitizationActionCount(envelope.Evidence.Sanitization.Truncations, "serialized_bytes"); count != 1 {
		t.Fatalf("serialized-byte truncation count=%d, want 1", count)
	}
}
