package traces

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/otelids"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// These three tools take caller-supplied tracejson and previously marshalled it
// straight into the request body, bypassing ID validation and the field-syntax
// and operator rewrites that get_traces already applied.

func TestGetTraceAttributesForPipeline_ValidatesTraceIDs(t *testing.T) {
	server, n, _ := recordingTraceDetailsServer(t)
	handler := NewGetTraceAttributesForPipelineHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributesForPipelineArgs{
		Pipeline: filterStage(map[string]interface{}{
			"$eq": []interface{}{"TraceId", testSpanIDAsTraceID},
		}),
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if !strings.Contains(err.Error(), "category="+otelids.CategorySpanIDAsTraceID) {
		t.Fatalf("want span-id-as-trace-id category, got %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("invalid ID made %d upstream requests, want 0", got)
	}
}

func TestGetTraceAttributeValues_ValidatesTraceIDs(t *testing.T) {
	server, n, _ := recordingTraceDetailsServer(t)
	handler := NewGetTraceAttributeValuesHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{
		TagName: "http.method",
		Pipeline: filterStage(map[string]interface{}{
			"$eq": []interface{}{"SpanId", testValidTraceID},
		}),
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if !strings.Contains(err.Error(), "category="+otelids.CategoryInvalidSpanID) {
		t.Fatalf("want invalid-span-id category, got %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("invalid ID made %d upstream requests, want 0", got)
	}
}

// deviations takes bare filter conditions rather than stages, so it goes through
// SanitizeTraceFilterConditions instead of SanitizeTraceJSONQuery.
func TestGetTraceAttributeDeviations_ValidatesFilterTraceIDs(t *testing.T) {
	_, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{"$eq": []interface{}{"TraceId", testSpanIDAsTraceID}},
		},
	}, time.Now())
	if err == nil {
		t.Fatal("expected local validation error")
	}
	// TraceId is disallowed for deviations entirely (upstream 422); denylist
	// runs before OTel ID normalization so the field error wins.
	if !strings.Contains(err.Error(), `invalid filter field "TraceId"`) {
		t.Fatalf("want TraceId field rejection, got %v", err)
	}
}

func TestGetTraceAttributeDeviations_RejectsFilterTraceIDs(t *testing.T) {
	args := GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{"$eq": []interface{}{"TraceId", strings.ToUpper(testValidTraceID)}},
		},
	}
	_, err := buildDeviationAPIRequest(args, time.Now())
	if err == nil {
		t.Fatal("expected TraceId filter to be rejected for deviations")
	}
	if !strings.Contains(err.Error(), `invalid filter field "TraceId"`) {
		t.Fatalf("want TraceId field rejection, got %v", err)
	}
}

// A bare condition inside a filters array must not be $and-wrapped: the wrap is
// only correct for a top-level filter stage query.
func TestSanitizeTraceFilterConditions_DoesNotWrapConditions(t *testing.T) {
	conditions := []map[string]interface{}{
		{"$eq": []interface{}{"ServiceName", "checkout"}},
	}
	sanitized, err := SanitizeTraceFilterConditions(conditions, "filters")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sanitized) != 1 {
		t.Fatalf("want 1 element, got %d: %v", len(sanitized), sanitized)
	}
	if _, wrapped := sanitized[0]["$and"]; wrapped {
		t.Errorf("condition was $and-wrapped: %v", sanitized[0])
	}
	if sanitized[0]["$eq"] == nil {
		t.Errorf("condition lost its operator: %v", sanitized[0])
	}
}

// A filters-array element that mixes $notnull/$exists with a sibling field
// operator is folded by the shared rewriteBrokenExistenceOperators helper into
// a single {"$and":[...]} map. A logical operator at the filters element level
// is non-canonical (the array is AND-implicit), so the sanitizer must split that
// $and back into sibling bare field-operator conditions instead of forwarding
// it. This test would have passed before the fix only because the old guard
// test never exercised the multi-operator-per-element case.
func TestSanitizeTraceFilterConditions_SplitsAndFoldFromExistenceRewrite(t *testing.T) {
	conditions := []map[string]interface{}{
		{
			"$notnull": []interface{}{"TraceId"},
			"$eq":      []interface{}{"ServiceName", "checkout"},
		},
	}
	sanitized, err := SanitizeTraceFilterConditions(conditions, "filters")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sanitized) != 2 {
		t.Fatalf("want the folded $and split into 2 bare elements, got %d: %v", len(sanitized), sanitized)
	}
	for i, cond := range sanitized {
		if _, hasAnd := cond["$and"]; hasAnd {
			t.Errorf("element %d is a $and logical operator (non-canonical): %v", i, cond)
		}
		if _, hasOr := cond["$or"]; hasOr {
			t.Errorf("element %d is a $or logical operator (non-canonical): %v", i, cond)
		}
		if _, hasNot := cond["$not"]; hasNot {
			t.Errorf("element %d is a $not logical operator (non-canonical): %v", i, cond)
		}
	}
	blob, _ := json.Marshal(sanitized)
	s := string(blob)
	if strings.Contains(s, "$notnull") || strings.Contains(s, "$exists") {
		t.Fatalf("broken existence operator survived rewrite: %s", s)
	}
	for _, want := range []string{
		`{"$eq":["ServiceName","checkout"]}`,
		`{"$neq":["TraceId",""]}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing expected bare condition %s in: %s", want, s)
		}
	}
}

// The split produced by the existence rewriter is AND-implicit, so splitting
// $and into sibling elements preserves semantics regardless of how the model
// bundled the operators.
func TestSanitizeTraceFilterConditions_SplitsExplicitAndElement(t *testing.T) {
	conditions := []map[string]interface{}{
		{"$and": []interface{}{
			map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}},
			map[string]interface{}{"$neq": []interface{}{"TraceId", ""}},
		}},
	}
	sanitized, err := SanitizeTraceFilterConditions(conditions, "filters")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sanitized) != 2 {
		t.Fatalf("want 2 bare elements, got %d: %v", len(sanitized), sanitized)
	}
	blob, _ := json.Marshal(sanitized)
	for _, want := range []string{
		`{"$eq":["ServiceName","checkout"]}`,
		`{"$neq":["TraceId",""]}`,
	} {
		if !strings.Contains(string(blob), want) {
			t.Fatalf("missing %s in: %s", want, blob)
		}
	}
}

// $or/$not cannot be split without changing semantics, so a logical operator
// that is not $and must be rejected with guidance rather than forwarded as a
// non-canonical filters element.
func TestSanitizeTraceFilterConditions_RejectsNonAndLogicalOperators(t *testing.T) {
	for _, op := range []string{"$or", "$not"} {
		conditions := []map[string]interface{}{
			{op: []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}},
		}
		if _, err := SanitizeTraceFilterConditions(conditions, "filters"); err == nil {
			t.Fatalf("%s element at filters element level must be rejected", op)
		}
	}
}

// The single-existence-operator case (no sibling) must still rewrite to a bare
// $neq element, unchanged by the split logic.
func TestSanitizeTraceFilterConditions_BareNotnullRewritesToBareNeq(t *testing.T) {
	conditions := []map[string]interface{}{
		{"$notnull": []interface{}{"TraceId"}},
	}
	sanitized, err := SanitizeTraceFilterConditions(conditions, "filters")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sanitized) != 1 {
		t.Fatalf("want 1 element, got %d: %v", len(sanitized), sanitized)
	}
	blob, _ := json.Marshal(sanitized)
	if want := `{"$neq":["TraceId",""]}`; !strings.Contains(string(blob), want) {
		t.Fatalf("want %s, got %s", want, blob)
	}
}

// A genuine $and with non-map children is malformed and must surface an error.
func TestSanitizeTraceFilterConditions_RejectsMalformedAndChildren(t *testing.T) {
	conditions := []map[string]interface{}{
		{"$and": []interface{}{"not-a-map"}},
	}
	if _, err := SanitizeTraceFilterConditions(conditions, "filters"); err == nil {
		t.Fatal("malformed $and children must be rejected")
	}
}

// The split produced by the existence rewriter sorts its children so the
// canonical output is byte-stable regardless of Go map-iteration order. Run the
// fold many times and assert identical bytes each iteration; a single run would
// hide nondeterminism because map iteration is randomized.
func TestSanitizeTraceFilterConditions_FoldOutputIsDeterministic(t *testing.T) {
	var want []byte
	for i := 0; i < 200; i++ {
		conditions := []map[string]interface{}{
			{
				"$notnull": []interface{}{"TraceId"},
				"$eq":      []interface{}{"ServiceName", "checkout"},
				"$neq":     []interface{}{"StatusCode", "STATUS_CODE_ERROR"},
			},
		}
		got, err := SanitizeTraceFilterConditions(conditions, "filters")
		if err != nil {
			t.Fatalf("iteration %d unexpected error: %v", i, err)
		}
		blob, _ := json.Marshal(got)
		if i == 0 {
			want = blob
			continue
		}
		if string(blob) != string(want) {
			t.Fatalf("iteration %d produced non-deterministic output:\nfirst: %s\nthis:  %s", i, want, blob)
		}
	}
	if len(want) == 0 {
		t.Fatal("determinism loop never ran")
	}
}
