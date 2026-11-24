package traces

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTracesPresencePredicateContract(t *testing.T) {
	for _, tc := range []struct {
		name, field, input string
		allowed            []string
	}{
		{"event exists", "events['phase']", "$exists", []string{"$exists", "$notnull"}},
		{"event notnull", "events['phase']", "$notnull", []string{"$exists", "$notnull"}},
		{"event name exists", "Events.Name", "$exists", []string{"$exists", "$notnull"}},
		{"span attribute exists", "attributes['phase']", "$exists", []string{"$notnull"}},
		{"resource attribute notnull", "resources['phase']", "$notnull", []string{"$notnull"}},
		{"explicit nonempty comparison", "attributes['phase']", "$neq", []string{"$neq"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operands := []any{tc.field}
			if tc.input == "$neq" {
				operands = append(operands, "")
			}
			query := map[string]any{"$and": []any{map[string]any{tc.input: operands}}}
			body, err := runTraceContractQuery(t, map[string]any{"type": "filter", "query": query})
			if err != nil || body == nil {
				t.Fatalf("expected a successful request, got body %v, error %v", body, err)
			}
			got := lastStage(t, body)["query"]
			for _, op := range tc.allowed {
				want := map[string]any{"$and": []any{map[string]any{op: operands}}}
				if reflect.DeepEqual(got, want) {
					return
				}
			}
			t.Errorf("forwarded %v, want %v applied to %v", got, tc.allowed, operands)
		})
	}
}

func TestGetTracesSplitIndexContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index float64
	}{
		{"first element", 1}, {"zero index", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stage := map[string]any{"type": "transform", "transforms": []any{map[string]any{
				"function": map[string]any{"$split": []any{"SpanName", "/", tc.index}}, "as": []any{"part"},
			}}}
			body, err := runTraceContractQuery(t, stage)
			if tc.index == 0 {
				if body != nil || err == nil || !strings.Contains(err.Error(), "$split") {
					t.Errorf("want local $split validation error with zero requests; got request %v, error %v", body != nil, err)
				}
				return
			}
			if err != nil || body == nil || !reflect.DeepEqual(lastStage(t, body), stage) {
				t.Errorf("valid split was not forwarded intact: body %v, error %v", body, err)
			}
		})
	}
}

func runTraceContractQuery(t *testing.T, stage map[string]any) (map[string]any, error) {
	t.Helper()
	rec := &selectBodyRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	defer srv.Close()
	result, _, err := NewGetTracesHandler(srv.Client(), testChunkTracesConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]any{stage},
		StartTimeISO:   "2026-01-01T00:00:00Z", EndTimeISO: "2026-01-01T00:01:00Z",
	})
	if result != nil && result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	switch len(rec.bodies) {
	case 0:
		return nil, err
	case 1:
		return rec.bodies[0], err
	default:
		t.Fatalf("single-minute query made %d requests", len(rec.bodies))
		return nil, nil
	}
}
