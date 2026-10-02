package logs

import (
	"encoding/json"
	"strings"
	"testing"

	"last9-mcp/internal/otelids"
)

const (
	logTestSpanIDAsTraceID = "abcdef0123456789"
	logTestValidTraceID    = "abcdef0123456789abcdef0123456789"
	logTestValidSpanID     = "0123456789abcdef"
)

func logFilterStage(query map[string]interface{}) []map[string]interface{} {
	return []map[string]interface{}{{"type": "filter", "query": query}}
}

// TraceId/SpanId/ParentSpanId are whitelisted as bare log fields for log-to-trace
// correlation, so a malformed ID here silently returns an empty result set.
func TestSanitizeLogJSONQueryRejectsMalformedTraceIdentifiers(t *testing.T) {
	cases := []struct {
		name     string
		query    map[string]interface{}
		category string
	}{
		{
			name:     "span id in TraceId",
			query:    map[string]interface{}{"$eq": []interface{}{"TraceId", logTestSpanIDAsTraceID}},
			category: otelids.CategorySpanIDAsTraceID,
		},
		{
			name:     "trace id in SpanId",
			query:    map[string]interface{}{"$eq": []interface{}{"SpanId", logTestValidTraceID}},
			category: otelids.CategoryInvalidSpanID,
		},
		{
			name:     "non-hex TraceId",
			query:    map[string]interface{}{"$eq": []interface{}{"TraceId", "not-a-trace-id-at-all-nope-nope"}},
			category: otelids.CategoryInvalidTraceID,
		},
		{
			name:     "all-zero TraceId",
			query:    map[string]interface{}{"$eq": []interface{}{"TraceId", strings.Repeat("0", 32)}},
			category: otelids.CategoryAllZeroID,
		},
		{
			name:     "malformed ParentSpanId",
			query:    map[string]interface{}{"$neq": []interface{}{"ParentSpanId", "abc"}},
			category: otelids.CategoryInvalidSpanID,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizeLogJSONQuery(logFilterStage(tt.query))
			if err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !strings.Contains(err.Error(), "category="+tt.category) {
				t.Fatalf("want category %s, got %v", tt.category, err)
			}
		})
	}
}

func TestSanitizeLogJSONQueryLowercasesTraceIdentifiers(t *testing.T) {
	sanitized, err := sanitizeLogJSONQuery(logFilterStage(map[string]interface{}{
		"$and": []interface{}{
			map[string]interface{}{"$eq": []interface{}{"TraceId", strings.ToUpper(logTestValidTraceID)}},
			map[string]interface{}{"$eq": []interface{}{"SpanId", strings.ToUpper(logTestValidSpanID)}},
		},
	}))
	if err != nil {
		t.Fatalf("valid uppercase IDs rejected: %v", err)
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), strings.ToUpper(logTestValidTraceID)) {
		t.Errorf("uppercase trace ID survived sanitization: %s", encoded)
	}
	if !strings.Contains(string(encoded), logTestValidTraceID) || !strings.Contains(string(encoded), logTestValidSpanID) {
		t.Errorf("IDs were not lowercased in place: %s", encoded)
	}
}

// Substring and regex operators legitimately match partial IDs, and an empty
// operand is the existence idiom rather than an ID.
func TestSanitizeLogJSONQueryAllowsPartialAndExistenceIDChecks(t *testing.T) {
	for _, query := range []map[string]interface{}{
		{"$contains": []interface{}{"TraceId", "abcdef"}},
		{"$regex": []interface{}{"TraceId", "^abc"}},
		{"$neq": []interface{}{"TraceId", ""}},
		{"$eq": []interface{}{"SpanId", ""}},
	} {
		if _, err := sanitizeLogJSONQuery(logFilterStage(query)); err != nil {
			t.Errorf("query %v should be allowed: %v", query, err)
		}
	}
}
