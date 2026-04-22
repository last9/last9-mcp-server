package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSanitizeLogJSONQueryNormalizesSafeAliases(t *testing.T) {
	sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{
						"$eq": []interface{}{"service.name", "checkout"},
					},
					map[string]interface{}{
						"$neq": []interface{}{"k8s.pod.name", ""},
					},
				},
			},
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{
						"$max": []interface{}{"attributes['duration']"},
					},
					"as": "max_duration",
				},
			},
			"groupby": map[string]interface{}{
				"k8s.namespace.name":  "namespace",
				"k8s.deployment.name": "deployment",
				"ServiceName":         "service",
			},
		},
	})
	if err != nil {
		t.Fatalf("sanitizeLogJSONQuery returned error: %v", err)
	}

	filterStage := sanitized[0]["query"].(map[string]interface{})
	andConditions := filterStage["$and"].([]interface{})

	serviceCondition := andConditions[0].(map[string]interface{})["$eq"].([]interface{})
	if got := serviceCondition[0]; got != "ServiceName" {
		t.Fatalf("expected service.name alias to normalize to ServiceName, got %#v", got)
	}

	podCondition := andConditions[1].(map[string]interface{})["$neq"].([]interface{})
	if got := podCondition[0]; got != "resource_attributes['k8s.pod.name']" {
		t.Fatalf("expected k8s.pod.name to normalize, got %#v", got)
	}

	groupBy := sanitized[1]["groupby"].(map[string]interface{})
	if _, ok := groupBy["resource_attributes['k8s.namespace.name']"]; !ok {
		t.Fatalf("expected namespace groupby to use resource_attributes syntax, got %#v", groupBy)
	}
	if _, ok := groupBy["resource_attributes['k8s.deployment.name']"]; !ok {
		t.Fatalf("expected deployment groupby to use resource_attributes syntax, got %#v", groupBy)
	}
	if _, ok := groupBy["ServiceName"]; !ok {
		t.Fatalf("expected canonical ServiceName groupby to be preserved, got %#v", groupBy)
	}
}

func TestSanitizeLogJSONQueryPreservesCanonicalRefs(t *testing.T) {
	input := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{
						"$eq": []interface{}{"ServiceName", "checkout"},
					},
					map[string]interface{}{
						"$gte": []interface{}{"attributes['http.status_code']", "500"},
					},
					map[string]interface{}{
						"$neq": []interface{}{"resource_attributes['k8s.namespace.name']", ""},
					},
				},
			},
		},
	}

	sanitized, err := sanitizeLogJSONQuery(input)
	if err != nil {
		t.Fatalf("sanitizeLogJSONQuery returned error: %v", err)
	}

	rawInput, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	rawSanitized, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatalf("failed to marshal sanitized query: %v", err)
	}

	if string(rawInput) != string(rawSanitized) {
		t.Fatalf("expected canonical refs to remain unchanged\ninput: %s\noutput: %s", rawInput, rawSanitized)
	}
}

func TestSanitizeLogJSONQueryRejectsUnsupportedBareDottedRefs(t *testing.T) {
	tests := []struct {
		name        string
		fieldRef    string
		expectedErr string
	}{
		{
			name:        "unsupported dotted field",
			fieldRef:    "deployment.environment",
			expectedErr: `invalid log field reference "deployment.environment"`,
		},
		{
			name:        "malformed canonical field",
			fieldRef:    `attributes['http.status_code']tail`,
			expectedErr: `invalid log field reference "attributes['http.status_code']tail"`,
		},
		{
			name:        "invalid kubernetes alias characters",
			fieldRef:    `k8s.namespace.name']`,
			expectedErr: `invalid log field reference "k8s.namespace.name']"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizeLogJSONQuery([]map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$and": []interface{}{
							map[string]interface{}{
								"$eq": []interface{}{tt.fieldRef, "prod"},
							},
						},
					},
				},
			})
			if err == nil {
				t.Fatal("expected sanitizeLogJSONQuery to reject invalid field refs")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Fatalf("expected invalid field error containing %q, got %v", tt.expectedErr, err)
			}
			if !strings.Contains(err.Error(), "get_log_attributes") {
				t.Fatalf("expected error to point callers to get_log_attributes, got %v", err)
			}
		})
	}
}

func TestSanitizeLogJSONQueryRejectsGroupByCollisions(t *testing.T) {
	_, err := sanitizeLogJSONQuery([]map[string]interface{}{
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{
						"$count": []interface{}{},
					},
					"as": "log_count",
				},
			},
			"groupby": map[string]interface{}{
				"service.name": "service_alias",
				"ServiceName":  "service",
			},
		},
	})
	if err == nil {
		t.Fatal("expected sanitizeLogJSONQuery to reject groupby collisions")
	}
	if !strings.Contains(err.Error(), "groupby collision") {
		t.Fatalf("expected groupby collision error, got %v", err)
	}
}

func TestGetLogsHandlerNormalizesAliasesBeforeAPICall(t *testing.T) {
	requestCount := 0
	handlerErr := make(chan error, 1)
	recordHandlerErr := func(err error) {
		select {
		case handlerErr <- err:
		default:
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Preflight prom query — respond gracefully without counting or validating.
		if r.URL.Path == constants.EndpointPromQueryInstant {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		requestCount++
		defer func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		}()

		var body struct {
			Pipeline []map[string]interface{} `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			recordHandlerErr(fmt.Errorf("failed to decode request body: %w", err))
			return
		}
		if len(body.Pipeline) < 2 {
			recordHandlerErr(fmt.Errorf("expected at least 2 pipeline stages, got %d", len(body.Pipeline)))
			return
		}

		filterStage, ok := body.Pipeline[0]["query"].(map[string]interface{})
		if !ok {
			recordHandlerErr(fmt.Errorf("expected first pipeline stage query map, got %T", body.Pipeline[0]["query"]))
			return
		}
		andConditions, ok := filterStage["$and"].([]interface{})
		if !ok || len(andConditions) == 0 {
			recordHandlerErr(fmt.Errorf("expected $and conditions in first pipeline stage, got %#v", filterStage["$and"]))
			return
		}
		firstCondition, ok := andConditions[0].(map[string]interface{})
		if !ok {
			recordHandlerErr(fmt.Errorf("expected first condition map, got %T", andConditions[0]))
			return
		}
		serviceCondition, ok := firstCondition["$eq"].([]interface{})
		if !ok || len(serviceCondition) == 0 {
			recordHandlerErr(fmt.Errorf("expected $eq service condition, got %#v", firstCondition["$eq"]))
			return
		}
		if got := serviceCondition[0]; got != "ServiceName" {
			recordHandlerErr(fmt.Errorf("expected service filter to use ServiceName, got %#v", got))
			return
		}

		groupBy, ok := body.Pipeline[1]["groupby"].(map[string]interface{})
		if !ok {
			recordHandlerErr(fmt.Errorf("expected second pipeline stage groupby map, got %T", body.Pipeline[1]["groupby"]))
			return
		}
		if _, ok := groupBy["resource_attributes['k8s.namespace.name']"]; !ok {
			recordHandlerErr(fmt.Errorf("expected groupby to include normalized k8s namespace key, got %#v", groupBy))
			return
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{
							"$eq": []interface{}{"service.name", "checkout"},
						},
					},
				},
			},
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{
							"$count": []interface{}{},
						},
						"as": "log_count",
					},
				},
				"groupby": map[string]interface{}{
					"k8s.namespace.name": "namespace",
				},
			},
		},
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	select {
	case err := <-handlerErr:
		t.Fatal(err)
	default:
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly one API request, got %d", requestCount)
	}
}

func TestGetLogsHandlerRejectsUnsupportedDottedRefsBeforeAPICall(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "should not be called", http.StatusBadRequest)
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{
							"$eq": []interface{}{"deployment.environment", "prod"},
						},
					},
				},
			},
		},
		LookbackMinutes: 5,
	})
	if err == nil {
		t.Fatal("expected handler to reject unsupported dotted field ref")
	}
	if requestCount != 0 {
		t.Fatalf("expected no API requests for invalid field refs, got %d", requestCount)
	}
}
