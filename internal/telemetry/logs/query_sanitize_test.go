package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	_, err := sanitizeLogJSONQuery([]map[string]interface{}{
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
	})
	if err == nil {
		t.Fatal("expected sanitizeLogJSONQuery to reject unsupported dotted field refs")
	}
	if !strings.Contains(err.Error(), `invalid log field reference "deployment.environment"`) {
		t.Fatalf("expected invalid field error, got %v", err)
	}
	if !strings.Contains(err.Error(), "get_log_attributes") {
		t.Fatalf("expected error to point callers to get_log_attributes, got %v", err)
	}
}

func TestGetLogsHandlerNormalizesAliasesBeforeAPICall(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		var body struct {
			Pipeline []map[string]interface{} `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		filterStage := body.Pipeline[0]["query"].(map[string]interface{})
		andConditions := filterStage["$and"].([]interface{})
		serviceCondition := andConditions[0].(map[string]interface{})["$eq"].([]interface{})
		if got := serviceCondition[0]; got != "ServiceName" {
			t.Fatalf("expected service filter to use ServiceName, got %#v", got)
		}

		groupBy := body.Pipeline[1]["groupby"].(map[string]interface{})
		if _, ok := groupBy["resource_attributes['k8s.namespace.name']"]; !ok {
			t.Fatalf("expected groupby to include normalized k8s namespace key, got %#v", groupBy)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
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
