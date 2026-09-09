package traces

import (
	"context"
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
	if !strings.Contains(err.Error(), "category="+otelids.CategorySpanIDAsTraceID) {
		t.Fatalf("want span-id-as-trace-id category, got %v", err)
	}
}

func TestGetTraceAttributeDeviations_NormalizesFilterTraceIDs(t *testing.T) {
	args := GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{"$eq": []interface{}{"TraceId", strings.ToUpper(testValidTraceID)}},
		},
	}
	request, err := buildDeviationAPIRequest(args, time.Now())
	if err != nil {
		t.Fatalf("valid uppercase ID rejected: %v", err)
	}
	condition, ok := request.Scope.Filters[0]["$eq"].([]interface{})
	if !ok || len(condition) != 2 {
		t.Fatalf("unexpected forwarded condition: %v", request.Scope.Filters[0])
	}
	if condition[1] != testValidTraceID {
		t.Errorf("want lowercased trace ID forwarded, got %v", condition[1])
	}
}

// A bare condition inside a filters array must not be $and-wrapped: the wrap is
// only correct for a top-level filter stage query.
func TestSanitizeTraceFilterConditions_DoesNotWrapConditions(t *testing.T) {
	conditions := []map[string]interface{}{
		{"$eq": []interface{}{"ServiceName", "checkout"}},
	}
	if err := SanitizeTraceFilterConditions(conditions, "filters"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, wrapped := conditions[0]["$and"]; wrapped {
		t.Errorf("condition was $and-wrapped: %v", conditions[0])
	}
	if conditions[0]["$eq"] == nil {
		t.Errorf("condition lost its operator: %v", conditions[0])
	}
}
