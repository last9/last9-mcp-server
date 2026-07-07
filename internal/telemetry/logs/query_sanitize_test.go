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
	if got := podCondition[0]; got != "resources['k8s.pod.name']" {
		t.Fatalf("expected k8s.pod.name to normalize, got %#v", got)
	}

	groupBy := sanitized[1]["groupby"].(map[string]interface{})
	if _, ok := groupBy["resources['k8s.namespace.name']"]; !ok {
		t.Fatalf("expected namespace groupby to use resources syntax, got %#v", groupBy)
	}
	if _, ok := groupBy["resources['k8s.deployment.name']"]; !ok {
		t.Fatalf("expected deployment groupby to use resources syntax, got %#v", groupBy)
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
						"$neq": []interface{}{"resources['k8s.namespace.name']", ""},
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

func TestSanitizeLogJSONQueryRejectsUnknownFilterConditionKeys(t *testing.T) {
	tests := []struct {
		name  string
		query map[string]interface{}
	}{
		{
			name:  "field as key with scalar",
			query: map[string]interface{}{"ServiceName": "checkout"},
		},
		{
			name: "field as key with nested operator",
			query: map[string]interface{}{
				"ServiceName": map[string]interface{}{"$eq": "checkout"},
			},
		},
		{
			name:  "operator suffix on field",
			query: map[string]interface{}{"ServiceName=~": "checkout"},
		},
		{
			name: "unknown key inside $and",
			query: map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"ServiceName": "checkout"},
				},
			},
		},
		{
			name:  "bare contains operator without dollar prefix",
			query: map[string]interface{}{"contains": []interface{}{"Body", "error"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizeLogJSONQuery([]map[string]interface{}{
				{
					"type":  "filter",
					"query": tt.query,
				},
			})
			if err == nil {
				t.Fatal("expected sanitizeLogJSONQuery to reject unknown filter key")
			}
			if !strings.Contains(err.Error(), "invalid filter condition key") {
				t.Fatalf("expected invalid filter key error, got %v", err)
			}
			if !strings.Contains(err.Error(), "get_log_attributes") {
				t.Fatalf("expected error to point callers to get_log_attributes, got %v", err)
			}
		})
	}
}

func TestSanitizeLogJSONQueryRejectsNonArrayFieldOperatorArgs(t *testing.T) {
	_, err := sanitizeLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": "checkout",
			},
		},
	})
	if err == nil {
		t.Fatal("expected sanitizeLogJSONQuery to reject non-array field operator args")
	}
	if !strings.Contains(err.Error(), "invalid arguments for field operator") {
		t.Fatalf("expected invalid args error, got %v", err)
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

func TestSanitizeLogJSONQueryRejectsDoubleQuotedBracketSyntax(t *testing.T) {
	tests := []struct {
		name     string
		fieldRef string
		wantHint string
	}{
		{
			name:     "attributes double-quoted",
			fieldRef: `attributes["http.method"]`,
			wantHint: `attributes['http.method']`,
		},
		{
			name:     "resources double-quoted",
			fieldRef: `resources["k8s.namespace.name"]`,
			wantHint: `resources['k8s.namespace.name']`,
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
								"$eq": []interface{}{tt.fieldRef, "value"},
							},
						},
					},
				},
			})
			if err == nil {
				t.Fatalf("expected error for double-quoted field ref %q, got nil", tt.fieldRef)
			}
			if !strings.Contains(err.Error(), "single quotes") {
				t.Errorf("expected error to mention single quotes, got: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantHint) {
				t.Errorf("expected error to contain corrected form %q, got: %v", tt.wantHint, err)
			}
		})
	}
}

func TestSanitizeLogJSONQueryRejectsFlatResourcePrefix(t *testing.T) {
	tests := []struct {
		name     string
		fieldRef string
		wantKey  string
	}{
		{
			name:     "resource_department",
			fieldRef: "resource_department",
			wantKey:  "resources['department']",
		},
		{
			name:     "resource_env",
			fieldRef: "resource_env",
			wantKey:  "resources['env']",
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
								"$eq": []interface{}{tt.fieldRef, "value"},
							},
						},
					},
				},
			})
			if err == nil {
				t.Fatalf("expected error for flat resource_ prefix %q, got nil", tt.fieldRef)
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("expected error to contain %q, got: %v", tt.wantKey, err)
			}
			if !strings.Contains(err.Error(), "get_log_attributes") {
				t.Errorf("expected error to point to get_log_attributes, got: %v", err)
			}
		})
	}
}

func TestSanitizeLogJSONQueryPreservesTopLevelFields(t *testing.T) {
	for _, field := range []string{"Body", "SeverityText", "Timestamp"} {
		t.Run(field, func(t *testing.T) {
			sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$neq": []interface{}{field, ""},
					},
				},
			})
			if err != nil {
				t.Fatalf("expected %q to pass unchanged, got error: %v", field, err)
			}
			args := sanitized[0]["query"].(map[string]interface{})["$neq"].([]interface{})
			if got := args[0]; got != field {
				t.Errorf("expected %q to be preserved unchanged, got %#v", field, got)
			}
		})
	}
}

func TestSanitizeLogJSONQueryAcceptsAllValidOperators(t *testing.T) {
	tests := []struct {
		name   string
		stages []map[string]interface{}
	}{
		{
			name: "case-insensitive equality operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$ieq": []interface{}{"ServiceName", "API"}},
						map[string]interface{}{"$ineq": []interface{}{"ServiceName", "nginx"}},
					},
				}},
			},
		},
		{
			name: "case-insensitive contains operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$icontains": []interface{}{"Body", "error"}},
						map[string]interface{}{"$inotcontains": []interface{}{"Body", "debug"}},
					},
				}},
			},
		},
		{
			name: "word-boundary operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$containsWords": []interface{}{"Body", "timeout"}},
						map[string]interface{}{"$icontainsWords": []interface{}{"Body", "error"}},
						map[string]interface{}{"$notcontainsWords": []interface{}{"Body", "debug"}},
						map[string]interface{}{"$inotcontainsWords": []interface{}{"Body", "trace"}},
					},
				}},
			},
		},
		{
			name: "case-insensitive regex operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$iregex": []interface{}{"Body", ".*error.*"}},
						map[string]interface{}{"$inotregex": []interface{}{"Body", ".*debug.*"}},
					},
				}},
			},
		},
		{
			name: "numeric comparison operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$gt": []interface{}{"attributes['http.status_code']", "400"}},
						map[string]interface{}{"$lte": []interface{}{"attributes['http.status_code']", "599"}},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := sanitizeLogJSONQuery(tt.stages); err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestSanitizeLogJSONQueryNormalizesInsideOrAndNot(t *testing.T) {
	t.Run("service.name inside $or normalizes to ServiceName", func(t *testing.T) {
		sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$or": []interface{}{
						map[string]interface{}{"$eq": []interface{}{"service.name", "checkout"}},
						map[string]interface{}{"$eq": []interface{}{"service.name", "payments"}},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		orConds := sanitized[0]["query"].(map[string]interface{})["$or"].([]interface{})
		first := orConds[0].(map[string]interface{})["$eq"].([]interface{})
		if got := first[0]; got != "ServiceName" {
			t.Errorf("expected service.name inside $or to normalize to ServiceName, got %#v", got)
		}
	})

	t.Run("k8s alias inside $not normalizes", func(t *testing.T) {
		sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$not": map[string]interface{}{
						"$eq": []interface{}{"k8s.pod.name", ""},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		notCond := sanitized[0]["query"].(map[string]interface{})["$not"].(map[string]interface{})
		args := notCond["$eq"].([]interface{})
		if got := args[0]; got != "resources['k8s.pod.name']" {
			t.Errorf("expected k8s alias inside $not to normalize, got %#v", got)
		}
	})

	t.Run("resource_ prefix inside $or is rejected", func(t *testing.T) {
		_, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$or": []interface{}{
						map[string]interface{}{"$eq": []interface{}{"resource_env", "prod"}},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for resource_ prefix inside $or, got nil")
		}
		if !strings.Contains(err.Error(), "resources['env']") {
			t.Errorf("expected error to mention resources['env'], got: %v", err)
		}
	})

	t.Run("double-quoted field inside $not is rejected", func(t *testing.T) {
		_, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$not": map[string]interface{}{
						"$eq": []interface{}{`attributes["env"]`, "prod"},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for double-quoted field inside $not, got nil")
		}
		if !strings.Contains(err.Error(), "single quotes") {
			t.Errorf("expected error to mention single quotes, got: %v", err)
		}
	})
}

func TestSanitizeLogJSONQueryNormalizesK8sAliasInAggregateFunctionArgs(t *testing.T) {
	t.Run("k8s alias in $avg arg is normalized", func(t *testing.T) {
		sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{
							"$avg": []interface{}{"k8s.namespace.name"},
						},
						"as": "avg_ns",
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		aggs := sanitized[0]["aggregates"].([]interface{})
		fn := aggs[0].(map[string]interface{})["function"].(map[string]interface{})
		args := fn["$avg"].([]interface{})
		if got := args[0]; got != "resources['k8s.namespace.name']" {
			t.Errorf("expected k8s alias in $avg to normalize, got %#v", got)
		}
	})

	t.Run("$quantile field arg at index 1 is normalized", func(t *testing.T) {
		sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{
							"$quantile": []interface{}{0.95, "k8s.cluster.name"},
						},
						"as": "p95",
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		aggs := sanitized[0]["aggregates"].([]interface{})
		fn := aggs[0].(map[string]interface{})["function"].(map[string]interface{})
		args := fn["$quantile"].([]interface{})
		if got := args[1]; got != "resources['k8s.cluster.name']" {
			t.Errorf("expected k8s alias at $quantile[1] to normalize, got %#v", got)
		}
		if got := args[0]; got != 0.95 {
			t.Errorf("expected $quantile[0] percentile to be unchanged, got %#v", got)
		}
	})

	t.Run("resource_ prefix in $sum arg is rejected", func(t *testing.T) {
		_, err := sanitizeLogJSONQuery([]map[string]interface{}{
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{
							"$sum": []interface{}{"resource_bytes"},
						},
						"as": "total_bytes",
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for resource_ prefix in aggregate function arg, got nil")
		}
		if !strings.Contains(err.Error(), "resources['bytes']") {
			t.Errorf("expected error to mention resources['bytes'], got: %v", err)
		}
	})
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
		requestCount++
		defer func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		}()

		// The count-sanity guardrail (single service + $count aggregate)
		// fires here and issues a second request, a baseline prometheus
		// instant query, at a different path on this same test server. Only
		// the logs query_range request carries a "pipeline" body to assert
		// on; let the baseline request through untouched.
		if r.URL.Path != constants.EndpointLogsQueryRange {
			return
		}

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
		if _, ok := groupBy["resources['k8s.namespace.name']"]; !ok {
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
	// 2 requests: the logs query_range call, plus the count-sanity
	// guardrail's baseline prometheus instant query (this pipeline has a
	// single ServiceName filter and a $count aggregate).
	if requestCount != 2 {
		t.Fatalf("expected exactly two API requests (logs + count-sanity baseline), got %d", requestCount)
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
