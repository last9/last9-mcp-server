package logs

import (
	"context"
	"encoding/json"
	"errors"
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
			// Return a minimal aggregate row so the count-sanity guardrail can
			// parse matchedCount and fire the baseline request.
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"log_count":1},"values":[]}]}}`))
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

func TestGetLogsHandlerRejectsUnsafeQuantilesBeforeAPICall(t *testing.T) {
	field := "attributes['latency_ms']"
	cases := []struct {
		name       string
		pipeline   []map[string]interface{}
		fieldInErr string
	}{
		{"attribute without guard", []map[string]interface{}{testQuantileAggregate(field)}, field},
		{"parse after guard", []map[string]interface{}{
			testCanonicalNumericFilter(field),
			{"type": "parse", "parser": "json", "field": "Body", "labels": map[string]interface{}{"latency_ms": "latency_ms"}},
			testQuantileAggregate(field),
		}, field},
		{"sanitized resource alias", []map[string]interface{}{testQuantileAggregate("k8s.pod.cpu")}, "resources['k8s.pod.cpu']"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertGetLogsQuantileRejected(t, tc.pipeline, tc.fieldInErr)
		})
	}
}

func assertGetLogsQuantileRejected(t *testing.T, pipeline []map[string]interface{}, fieldInErr string) {
	t.Helper()
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "should not be called", http.StatusBadRequest)
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: pipeline, LookbackMinutes: 5})
	if err == nil || !strings.Contains(err.Error(), fieldInErr) || !strings.Contains(err.Error(), "canonical anchored numeric $regex") {
		t.Fatalf("expected field-specific canonical regex error, got: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no API requests, got %d", requestCount)
	}
}

func TestGetLogsHandlerRejectsBodyQuantileWhenParseFollowsNumericRegex(t *testing.T) {
	field := "attributes['latency_ms']"
	assertGetLogsQuantileRejected(t, []map[string]interface{}{
		testCanonicalNumericFilter(field),
		{"type": "parse", "parser": "json", "field": "Body", "labels": map[string]interface{}{"latency_ms": "latency_ms"}},
		testQuantileAggregate(field),
	}, field)
}

func testQuantileAggregate(field string) map[string]interface{} {
	return map[string]interface{}{
		"type": "aggregate",
		"aggregates": []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{"$quantile": []interface{}{0.99, field}},
				"as":       "p99",
			},
		},
	}
}

func testCanonicalNumericFilter(field string) map[string]interface{} {
	return testNumericFilter(field, `^[0-9]+(?:\.[0-9]+)?$`)
}

func testNumericFilter(field, pattern string) map[string]interface{} {
	return map[string]interface{}{
		"type": "filter",
		"query": map[string]interface{}{
			"$and": []interface{}{
				map[string]interface{}{"$regex": []interface{}{field, pattern}},
			},
		},
	}
}

func TestGetLogsHandlerGuardsSanitizedResourceAliasQuantile(t *testing.T) {
	assertGetLogsQuantileRejected(t, []map[string]interface{}{testQuantileAggregate("k8s.pod.cpu")}, "resources['k8s.pod.cpu']")
}

func TestPrepareLogJSONQueryParseInvalidatesNumericSafeState(t *testing.T) {
	field := "attributes['latency_ms']"
	for _, tc := range []struct {
		name  string
		parse map[string]interface{}
	}{
		{
			name: "same declared JSON field",
			parse: map[string]interface{}{
				"type": "parse", "parser": "json", "field": "Body",
				"labels": map[string]interface{}{"latency_ms": "latency_ms"},
			},
		},
		{
			name:  "JSON without labels invalidates all attributes",
			parse: map[string]interface{}{"type": "parse", "parser": "json", "field": "Body"},
		},
		{
			name:  "logfmt without labels invalidates all attributes",
			parse: map[string]interface{}{"type": "parse", "parser": "logfmt", "field": "Body"},
		},
		{
			name: "regexp named capture",
			parse: map[string]interface{}{
				"type": "parse", "parser": "regexp", "field": "Body",
				"pattern": `latency=(?P<latency_ms>[0-9]+)`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPrepareQuantileRejected(t, []map[string]interface{}{
				testCanonicalNumericFilter(field),
				tc.parse,
				testQuantileAggregate(field),
			})
		})
	}
}

func TestGetLogsHandlerNonChunkedAggregateProbesAndAnnotatesRowLimit(t *testing.T) {
	assertAggregateLimitCase(t, 2, 0, "3", `[{"bucket":"a"},{"bucket":"b"},{"bucket":"probe"}]`, 2, true)
}

func assertAggregateLimitCase(t *testing.T, requested, configuredMax int, wantProbe, rowsJSON string, wantRows int, wantPartial bool) {
	t.Helper()
	receivedLimit := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedLimit <- r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"dataframe","result":%s}}`, rowsJSON)
	}))
	defer server.Close()

	cfg := testLogsConfig(server.URL)
	cfg.MaxGetLogsEntries = configuredMax
	handler := NewGetLogsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{testCountAggregate()}, LookbackMinutes: 5, Limit: requested,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if got := <-receivedLimit; got != wantProbe {
		t.Fatalf("probe limit=%q, want %q", got, wantProbe)
	}
	body := decodeGetLogsBody(t, result)
	rows := body["data"].(map[string]interface{})["result"].([]interface{})
	if len(rows) != wantRows {
		t.Fatalf("returned rows=%d, want %d", len(rows), wantRows)
	}
	meta, partial := body["l9_result"].(map[string]interface{})
	if partial != wantPartial || (partial && (meta["partial"] != true || meta["reason"] != "row_limit_reached")) {
		t.Fatalf("partial metadata=%#v, wantPartial=%v", meta, wantPartial)
	}
}

func testCountAggregate() map[string]interface{} {
	return map[string]interface{}{
		"type": "aggregate",
		"aggregates": []interface{}{map[string]interface{}{
			"function": map[string]interface{}{"$count": []interface{}{}}, "as": "row_count",
		}},
	}
}

func decodeGetLogsBody(t *testing.T, result *mcp.CallToolResult) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &body); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	return body
}

func TestGetLogsHandlerAcceptsSafeQuantileInputs(t *testing.T) {
	field := "attributes['latency_ms']"
	numericFilter, aggregate := testCanonicalNumericFilter(field), testQuantileAggregate(field)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"dataframe","result":[]}}`))
	}))
	defer server.Close()
	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))

	for _, tc := range []struct {
		name   string
		stages []map[string]interface{}
	}{
		{
			name:   "indexed attribute with preceding numeric regex",
			stages: []map[string]interface{}{numericFilter, aggregate},
		},
		{
			name: "Body parse before numeric regex",
			stages: []map[string]interface{}{
				{
					"type":   "parse",
					"parser": "json",
					"field":  "Body",
					"labels": map[string]interface{}{"latency_ms": "latency_ms"},
				},
				numericFilter,
				aggregate,
			},
		},
		{
			name:   "known top-level numeric field without regex",
			stages: []map[string]interface{}{testQuantileAggregate("Timestamp")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery:    tc.stages,
				LookbackMinutes: 5,
			})
			if err != nil {
				t.Fatalf("handler rejected safe quantile pipeline: %v", err)
			}
		})
	}
	if requestCount != 3 {
		t.Fatalf("expected one API request per safe case, got %d", requestCount)
	}
}

func TestPrepareLogJSONQueryRejectsNonNumericRegexForAttributeQuantile(t *testing.T) {
	field := "attributes['latency_ms']"
	assertPrepareQuantileRejected(t, []map[string]interface{}{testNumericFilter(field, `^.+$`), testQuantileAggregate(field)})
}

func assertPrepareQuantileRejected(t *testing.T, pipeline []map[string]interface{}) {
	t.Helper()
	_, err := prepareLogJSONQuery(pipeline, "logjson_query")
	if err == nil || !strings.Contains(err.Error(), "canonical anchored numeric $regex") {
		t.Fatalf("expected canonical numeric-regex error, got: %v", err)
	}
}

func TestPrepareLogJSONQueryRejectsNonMandatoryOrNonNumericRegexGuards(t *testing.T) {
	field := "attributes['latency_ms']"
	aggregate := testQuantileAggregate(field)

	for _, tc := range []struct {
		name  string
		query map[string]interface{}
	}{
		{
			name: "regex in or is not mandatory",
			query: map[string]interface{}{
				"$or": []interface{}{
					map[string]interface{}{"$regex": []interface{}{field, `^[0-9]+(?:\.[0-9]+)?$`}},
					map[string]interface{}{"$neq": []interface{}{"SeverityText", ""}},
				},
			},
		},
		{
			name: "regex in not is negative",
			query: map[string]interface{}{
				"$not": map[string]interface{}{"$regex": []interface{}{field, `^[0-9]+(?:\.[0-9]+)?$`}},
			},
		},
		{
			name:  "alternation permits a nonnumeric literal",
			query: map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$regex": []interface{}{field, `^(?:[0-9]+|unknown)$`}}}},
		},
		{
			name:  "literal suffix permits nonnumeric values",
			query: map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$regex": []interface{}{field, `^[0-9]+ms$`}}}},
		},
		{
			name:  "broad character class permits nonnumeric values",
			query: map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$regex": []interface{}{field, `^[[:alnum:]]+$`}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPrepareQuantileRejected(t, []map[string]interface{}{
				{"type": "filter", "query": tc.query},
				aggregate,
			})
		})
	}
}

func TestGetLogsHandlerPartialGroupedCountSkipsCountSanity(t *testing.T) {
	logsRequests := 0
	baselineRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			logsRequests++
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"dataframe","result":[{"metric":{"row_count":7,"route":"/a"},"values":[]},{"metric":{"row_count":11,"route":"/b"},"values":[]}]}}`))
		case constants.EndpointPromQueryInstant:
			baselineRequests++
			_, _ = w.Write([]byte(`[{"metric":{},"value":[1700000000,"100"]}]`))
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", "example-service"}}},
				},
			},
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "row_count"},
				},
				"groupby": map[string]interface{}{"attributes['route']": "route"},
			},
		},
		LookbackMinutes: 5,
		Limit:           1,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if logsRequests != 1 {
		t.Fatalf("expected one logs request, got %d", logsRequests)
	}
	if baselineRequests != 0 {
		t.Fatalf("partial aggregate must skip count sanity baseline, got %d requests", baselineRequests)
	}

	body := decodeGetLogsBody(t, result)
	if _, exists := body["l9_sanity"]; exists {
		t.Fatalf("partial aggregate must not contain l9_sanity: %#v", body["l9_sanity"])
	}
	meta := body["l9_result"].(map[string]interface{})
	if meta["partial"] != true {
		t.Fatalf("expected truthful partial metadata, got %#v", meta)
	}
}

func TestGetLogsHandlerAggregateLimitDefaultsCapsAndLeavesUnderLimitUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name          string
		requested     int
		configuredMax int
		wantProbe     string
	}{
		{name: "omitted uses backend default plus probe", wantProbe: "1001"},
		{name: "omitted default is capped by configured max", configuredMax: 100, wantProbe: "101"},
		{name: "explicit limit is capped before probe", requested: 10, configuredMax: 2, wantProbe: "3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertAggregateLimitCase(t, tc.requested, tc.configuredMax, tc.wantProbe, `[{"bucket":"only"}]`, 1, false)
		})
	}
}

// TestPrepareLogJSONQueryValidation covers the fail-closed validation rules
// introduced by prepareLogJSONQuery / validateLogJSONQuery.
func TestPrepareLogJSONQueryValidation(t *testing.T) {
	type tc struct {
		name      string
		stages    []map[string]interface{}
		wantErr   bool
		errSubstr string
	}

	cases := []tc{
		{
			name: "canonical window_aggregate accepted",
			stages: []map[string]interface{}{
				{
					"type":  "filter",
					"query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"SeverityText", "ERROR"}}}},
				},
				{
					"type":     "window_aggregate",
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "errors",
					"window":   []interface{}{"1", "minutes"},
				},
			},
			wantErr: false,
		},
		{
			name: "window_minutes on window_aggregate rejected",
			stages: []map[string]interface{}{
				{
					"type":           "window_aggregate",
					"function":       map[string]interface{}{"$count": []interface{}{}},
					"as":             "errors",
					"window_minutes": 1,
				},
			},
			wantErr:   true,
			errSubstr: "window_minutes",
		},
		{
			name: "aggregates on window_aggregate rejected",
			stages: []map[string]interface{}{
				{
					"type": "window_aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "c"},
					},
					"as":     "errors",
					"window": []interface{}{"1", "minutes"},
				},
			},
			wantErr:   true,
			errSubstr: "aggregates",
		},
		{
			name: "format on parse stage rejected",
			stages: []map[string]interface{}{
				{
					"type":   "parse",
					"format": "json",
					"field":  "Body",
				},
			},
			wantErr:   true,
			errSubstr: "format",
		},
		{
			name: "missing parser on parse stage rejected",
			stages: []map[string]interface{}{
				{
					"type":  "parse",
					"field": "Body",
				},
			},
			wantErr:   true,
			errSubstr: "parser",
		},
		{
			name: "SpanKind filter rejected",
			stages: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$and": []interface{}{
							map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "SpanKind",
		},
		{
			name: "StatusCode filter rejected",
			stages: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$and": []interface{}{
							map[string]interface{}{"$eq": []interface{}{"StatusCode", "STATUS_CODE_ERROR"}},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "StatusCode",
		},
		{
			name: "valid parse with parser accepted",
			stages: []map[string]interface{}{
				{
					"type":   "parse",
					"parser": "json",
					"field":  "Body",
				},
			},
			wantErr: false,
		},
		{
			name: "service.name alias still normalizes via prepare (sanitize runs after validate)",
			stages: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$and": []interface{}{
							map[string]interface{}{"$eq": []interface{}{"service.name", "api"}},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := prepareLogJSONQuery(c.stages, "logjson_query")
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if c.errSubstr != "" && !strings.Contains(err.Error(), c.errSubstr) {
					t.Errorf("error %q missing expected substring %q", err.Error(), c.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}
}

func TestPrepareLogJSONQueryValidatesQuantileArguments(t *testing.T) {
	aggregate := func(args interface{}) []map[string]interface{} {
		return []map[string]interface{}{{
			"type": "aggregate",
			"aggregates": []interface{}{map[string]interface{}{
				"function": map[string]interface{}{"$quantile": args},
				"as":       "p99",
			}},
		}}
	}
	windowAggregate := func(args interface{}) []map[string]interface{} {
		return []map[string]interface{}{{
			"type":     "window_aggregate",
			"function": map[string]interface{}{"$quantile": args},
			"as":       "p99",
			"window":   []interface{}{"24", "hours"},
		}}
	}

	for _, tc := range []struct {
		name    string
		stages  []map[string]interface{}
		wantErr bool
	}{
		{name: "aggregate accepts lower level boundary then known top-level numeric field", stages: aggregate([]interface{}{0.0, "Timestamp"})},
		{name: "aggregate rejects swapped args", stages: aggregate([]interface{}{"attributes['latency_ms']", 0.99}), wantErr: true},
		{name: "aggregate rejects wrong arity", stages: aggregate([]interface{}{0.99}), wantErr: true},
		{name: "aggregate rejects level outside range", stages: aggregate([]interface{}{1.01, "attributes['latency_ms']"}), wantErr: true},
		{name: "window accepts upper level boundary then known top-level numeric field", stages: windowAggregate([]interface{}{1.0, "Timestamp"})},
		{name: "window rejects swapped args", stages: windowAggregate([]interface{}{"attributes['latency_ms']", 0.99}), wantErr: true},
		{name: "window rejects wrong arity", stages: windowAggregate([]interface{}{0.99, "attributes['latency_ms']", "extra"}), wantErr: true},
		{name: "window rejects non-string field", stages: windowAggregate([]interface{}{0.99, 42.0}), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareLogJSONQuery(tc.stages, "logjson_query")
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "[level, field]") {
				t.Fatalf("error must show the correct argument order, got: %v", err)
			}
		})
	}
}

// TestPrepareLogJSONQueryErrorsNoQueryBuilder asserts that the empty-query path
// in NewGetLogsHandler no longer references the logjson_query_builder prompt.
func TestPrepareLogJSONQueryErrorsNoQueryBuilder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusBadRequest)
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{})
	if err == nil {
		t.Fatal("expected error for empty logjson_query")
	}
	if strings.Contains(err.Error(), "query_builder") {
		t.Errorf("empty logjson_query error must not reference 'query_builder', got: %q", err.Error())
	}
}

func TestUnknownKeyTipsAreStageSpecific(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{"type": "parse", "format": "json", "field": "Body"},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected parse format rejection")
	}
	msg := err.Error()
	if strings.Contains(msg, "window_aggregate uses") {
		t.Errorf("parse unknown-key error must not include window_aggregate tip, got: %q", msg)
	}
	if !strings.Contains(msg, "never \"format\"") {
		t.Errorf("parse format error should mention never format, got: %q", msg)
	}

	_, err = prepareLogJSONQuery([]map[string]interface{}{
		{
			"type":           "window_aggregate",
			"function":       map[string]interface{}{"$count": []interface{}{}},
			"as":             "errors",
			"window_minutes": 1,
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected window_minutes rejection")
	}
	msg = err.Error()
	if !strings.Contains(msg, "window_aggregate uses") {
		t.Errorf("window_aggregate unknown-key error should include WA tip, got: %q", msg)
	}
}

func TestPrepareLogJSONQuerySanitizeUsesPathPrefix(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{`attributes["http.status_code"]`, "500"}},
				},
			},
		},
	}, "pipeline")
	if err == nil {
		t.Fatal("expected sanitize rejection for double-quoted attribute ref")
	}
	if !strings.Contains(err.Error(), "pipeline[0]") {
		t.Errorf("sanitize failure for attrs tool must use pipeline path prefix, got: %q", err.Error())
	}
	if strings.Contains(err.Error(), "logjson_query[0]") {
		t.Errorf("sanitize failure must not hardcode logjson_query when prefix is pipeline, got: %q", err.Error())
	}
}

func TestPrepareLogJSONQueryWrapsBareFilterInAnd(t *testing.T) {
	result, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"SeverityText", "ERROR"},
			},
		},
	}, "logjson_query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	query := result[0]["query"].(map[string]interface{})
	andConditions, ok := query["$and"].([]interface{})
	if !ok {
		t.Fatalf("expected top-level $and wrap, got %#v", query)
	}
	if len(andConditions) != 1 {
		t.Fatalf("expected 1 $and condition, got %#v", andConditions)
	}
	cond := andConditions[0].(map[string]interface{})
	if _, ok := cond["$eq"]; !ok {
		t.Fatalf("expected wrapped $eq condition, got %#v", cond)
	}
}

func TestPrepareLogJSONQueryRejectsBareSingleTokenField(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"community_member_id", "81453836"}},
				},
			},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected bare community_member_id to be rejected")
	}
	if !strings.Contains(err.Error(), "community_member_id") {
		t.Errorf("error should mention field name, got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "attributes['community_member_id']") {
		t.Errorf("error should suggest attributes['…'] form, got: %q", err.Error())
	}
}

func TestPrepareLogJSONQueryDefaultsMissingParseField(t *testing.T) {
	result, err := prepareLogJSONQuery([]map[string]interface{}{
		{"type": "parse", "parser": "json"},
	}, "logjson_query")
	if err != nil {
		t.Fatalf("missing parse field should default to Body, got error: %v", err)
	}
	if result[0]["field"] != "Body" {
		t.Errorf("expected field to default to Body, got %v", result[0]["field"])
	}
}

func TestPrepareLogJSONQueryRejectsBadRegexpNamedCapture(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type":    "parse",
			"parser":  "regexp",
			"field":   "Body",
			"pattern": `merchant_name (?P<$merchant>\S+)`,
			"labels":  map[string]interface{}{"merchant": "Body"},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected bad named capture rejection")
	}
	if !strings.Contains(err.Error(), "$merchant") && !strings.Contains(err.Error(), "named capture") {
		t.Errorf("error should mention invalid named capture, got: %q", err.Error())
	}
}

func TestPrepareLogJSONQueryRejectsDollarLabelKey(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
			"labels": map[string]interface{}{"$merchant": "merchant"},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected invalid labels key rejection")
	}
	if !strings.Contains(err.Error(), "$merchant") {
		t.Errorf("error should mention $merchant, got: %q", err.Error())
	}
}

func TestPrepareLogJSONQueryRejectsSpanNameGroupBy(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "error_count",
				},
			},
			"groupby": map[string]interface{}{"SpanName": "span_name"},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected SpanName groupby rejection")
	}
	if !strings.Contains(err.Error(), "SpanName") {
		t.Errorf("error should mention SpanName, got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "trace-only") {
		t.Errorf("error should say trace-only, got: %q", err.Error())
	}
}

func TestPrepareLogJSONQueryCoercesNumericFilterValues(t *testing.T) {
	result, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"attributes['status_code']", float64(500)}},
				},
			},
		},
	}, "logjson_query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	query := result[0]["query"].(map[string]interface{})
	andConditions := query["$and"].([]interface{})
	eqArgs := andConditions[0].(map[string]interface{})["$eq"].([]interface{})
	if got, ok := eqArgs[1].(string); !ok || got != "500" {
		t.Fatalf("expected numeric 500 coerced to string \"500\", got %#v", eqArgs[1])
	}
}

func TestPrepareLogJSONQueryAcceptsCanonicalParseThenFilter(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"ServiceName", "orders-api"}},
				},
			},
		},
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
			"labels": map[string]interface{}{
				"status_code": "status_code",
				"uri":         "uri",
			},
		},
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"attributes['status_code']", float64(500)}},
				},
			},
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
			"groupby": map[string]interface{}{"attributes['uri']": "uri"},
		},
	}, "logjson_query")
	if err != nil {
		t.Fatalf("canonical parse+filter+aggregate pipeline should pass after coerce+field require, got: %v", err)
	}
}

func TestPrepareLogJSONQueryTypedValidationError(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type":           "window_aggregate",
			"function":       map[string]interface{}{"$count": []interface{}{}},
			"as":             "errors",
			"window_minutes": 1,
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected validation error")
	}
	var typed *LogPipelineValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *LogPipelineValidationError, got %T (%v)", err, err)
	}
	if typed.Category != LogValidationUnknownStageKey {
		t.Errorf("category=%q, want %q", typed.Category, LogValidationUnknownStageKey)
	}
	if typed.Path == "" {
		t.Error("typed error Path must be non-empty")
	}
	if !strings.Contains(typed.Message, "window_minutes") {
		t.Errorf("message should mention window_minutes, got: %q", typed.Message)
	}
}

func TestPrepareLogJSONQueryTypedWrongDomainField(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
				},
			},
		},
	}, "pipeline")
	if err == nil {
		t.Fatal("expected SpanKind rejection")
	}
	var typed *LogPipelineValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *LogPipelineValidationError, got %T", err)
	}
	if typed.Category != LogValidationWrongDomainField {
		t.Errorf("category=%q, want %q", typed.Category, LogValidationWrongDomainField)
	}
}

// TestPrepareLogJSONQueryAcceptsTraceIdFilter verifies that TraceId, SpanId, and
// ParentSpanId are accepted as log field references for log↔trace correlation.
func TestPrepareLogJSONQueryAcceptsTraceIdFilter(t *testing.T) {
	for _, field := range []string{"TraceId", "SpanId", "ParentSpanId"} {
		t.Run(field, func(t *testing.T) {
			_, err := prepareLogJSONQuery([]map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$and": []interface{}{
							map[string]interface{}{"$neq": []interface{}{field, ""}},
						},
					},
				},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{
							"function": map[string]interface{}{"$count": []interface{}{}},
							"as":       "log_count",
						},
					},
				},
			}, "logjson_query")
			if err != nil {
				t.Fatalf("%s filter should be accepted for log-trace correlation, got: %v", field, err)
			}
		})
	}
}

// TestPrepareLogJSONQueryAcceptsDottedParseLabelKey verifies that dotted label
// keys like http.status_code are accepted in parse stage labels (safeBodyKeyPattern).
func TestPrepareLogJSONQueryAcceptsDottedParseLabelKey(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
			"labels": map[string]interface{}{"http.status_code": "status_code"},
		},
	}, "logjson_query")
	if err != nil {
		t.Fatalf("dotted label key http.status_code should be accepted, got: %v", err)
	}
}

// TestPrepareLogJSONQueryRoundTripBodyDerivedHint verifies that the two-stage
// pipeline shape emitted by bodyDerivedHint (parse json + filter on derived attr)
// passes prepareLogJSONQuery without error when a dotted key is used.
func TestPrepareLogJSONQueryRoundTripBodyDerivedHint(t *testing.T) {
	stages := []map[string]interface{}{
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
			"labels": map[string]interface{}{"http.status_code": "http.status_code"},
		},
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"attributes['http.status_code']", "500"}},
				},
			},
		},
	}
	_, err := prepareLogJSONQuery(stages, "logjson_query")
	if err != nil {
		t.Fatalf("body-derived hint round-trip should pass: %v", err)
	}
}

// TestPrepareLogJSONQueryFilterQueryRequired verifies that a filter stage missing
// "query" is rejected with a tip mentioning "conditions".
func TestPrepareLogJSONQueryFilterQueryRequired(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{"type": "filter"},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected error for filter stage missing query")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error should mention \"query\", got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "conditions") {
		t.Errorf("error should tip \"conditions\", got: %q", err.Error())
	}
}

// TestPrepareLogJSONQueryConditionsRejectsWithTip verifies that "conditions" (a
// common wrong key) is rejected before validation with a clear tip.
func TestPrepareLogJSONQueryConditionsRejectsWithTip(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type":       "filter",
			"conditions": map[string]interface{}{"$and": []interface{}{}},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected error for filter with conditions instead of query")
	}
	if !strings.Contains(err.Error(), "conditions") {
		t.Errorf("error should mention \"conditions\", got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error should tip to use \"query\", got: %q", err.Error())
	}
}

// TestPrepareLogJSONQueryAggregatesRequired verifies that an aggregate stage
// missing "aggregates" is rejected with tips for "aggs"/"aggregations".
func TestPrepareLogJSONQueryAggregatesRequired(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{"type": "aggregate"},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected error for aggregate stage missing aggregates")
	}
	if !strings.Contains(err.Error(), "aggregates") {
		t.Errorf("error should mention \"aggregates\", got: %q", err.Error())
	}
}

// TestPrepareLogJSONQueryAggsRejectsWithTip verifies that "aggs" is rejected
// with a tip to use "aggregates".
func TestPrepareLogJSONQueryAggsRejectsWithTip(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "aggregate",
			"aggs": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "c",
				},
			},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected error for aggregate with aggs instead of aggregates")
	}
	if !strings.Contains(err.Error(), "aggs") {
		t.Errorf("error should mention \"aggs\", got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "aggregates") {
		t.Errorf("error should tip to use \"aggregates\", got: %q", err.Error())
	}
}

// TestPrepareLogJSONQueryAliasRejectsWithTip verifies that "alias" in an
// aggregates item is rejected with a tip to use "as".
func TestPrepareLogJSONQueryAliasRejectsWithTip(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"alias":    "count",
				},
			},
		},
	}, "logjson_query")
	if err == nil {
		t.Fatal("expected error for aggregate item with alias instead of as")
	}
	if !strings.Contains(err.Error(), "alias") {
		t.Errorf("error should mention \"alias\", got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "as") {
		t.Errorf("error should tip to use \"as\", got: %q", err.Error())
	}
}

// TestPrepareLogJSONQueryParseFieldNoCallerMutation verifies that the caller's
// original stage map is NOT mutated when "field" is defaulted to "Body".
func TestPrepareLogJSONQueryParseFieldNoCallerMutation(t *testing.T) {
	original := map[string]interface{}{
		"type":   "parse",
		"parser": "json",
		// no "field" key — should be defaulted in returned pipeline only
	}
	stages := []map[string]interface{}{original}

	result, err := prepareLogJSONQuery(stages, "logjson_query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Returned pipeline must have "field" defaulted to "Body".
	if result[0]["field"] != "Body" {
		t.Errorf("expected returned stage to have field=Body, got %v", result[0]["field"])
	}

	// Caller's original map must NOT have been mutated.
	if _, mutated := original["field"]; mutated {
		t.Errorf("caller's original map was mutated: got field=%v", original["field"])
	}
}

// TestPrepareLogJSONQueryCatchAllTips verifies that inputs which pass schema
// validation via the catch-all anyOf branch are still rejected by
// validateLogJSONQuery with actionable tips — and never produce "anonymous
// schema" error messages that the model cannot act on.
func TestPrepareLogJSONQueryCatchAllTips(t *testing.T) {
	type tc struct {
		name      string
		stages    []map[string]interface{}
		errSubstr string
	}

	cases := []tc{
		{
			name: "filter query as SQL string",
			stages: []map[string]interface{}{
				{"type": "filter", "query": "SELECT * FROM logs WHERE SeverityText = 'ERROR'"},
			},
			errSubstr: "NOT SQL",
		},
		{
			name: "type sort (unknown stage type)",
			stages: []map[string]interface{}{
				{"type": "sort"},
			},
			errSubstr: "unknown stage type",
		},
		{
			name: "stage with no type field",
			stages: []map[string]interface{}{
				{"stage": "filter", "query": map[string]interface{}{"$and": []interface{}{}}},
			},
			errSubstr: "missing or non-string",
		},
		{
			name: "parser grok (invalid parser value)",
			stages: []map[string]interface{}{
				{"type": "parse", "parser": "grok"},
			},
			errSubstr: "grok",
		},
		{
			name: "function as string on window_aggregate",
			stages: []map[string]interface{}{
				{"type": "window_aggregate", "function": "$count", "as": "errors", "window": []interface{}{"1", "minutes"}},
			},
			errSubstr: "function",
		},
		{
			name: "window as string on window_aggregate",
			stages: []map[string]interface{}{
				{"type": "window_aggregate", "function": map[string]interface{}{"$count": []interface{}{}}, "as": "errors", "window": "1m"},
			},
			errSubstr: "window",
		},
		{
			name: "groupby as array on aggregate",
			stages: []map[string]interface{}{
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "c"},
					},
					"groupby": []interface{}{"ServiceName"},
				},
			},
			errSubstr: "groupby",
		},
		{
			name: "groupby as string on window_aggregate",
			stages: []map[string]interface{}{
				{
					"type":     "window_aggregate",
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "errors",
					"window":   []interface{}{"1", "minutes"},
					"groupby":  "ServiceName",
				},
			},
			errSubstr: "groupby",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := prepareLogJSONQuery(c.stages, "logjson_query")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.errSubstr) {
				t.Errorf("error %q missing expected substring %q", msg, c.errSubstr)
			}
			if strings.Contains(msg, "anonymous schema") {
				t.Errorf("error must not contain 'anonymous schema' (opaque validator message leaking through): %q", msg)
			}
		})
	}
}
