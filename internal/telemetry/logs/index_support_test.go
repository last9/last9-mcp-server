package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsHandler_ForwardsIndexAndBuildsResolvedDeepLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("index"); got != "physical_index:payments" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
		case "/logs_settings/physical_indexes":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"properties":[{"id":"idx-123","name":"payments"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$eq": []interface{}{"ServiceName", "api"},
				},
			},
		},
		LookbackMinutes: 5,
		Index:           "physical_index:payments",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := referenceURL(t, result); got == "" {
		t.Fatal("expected reference_url in result meta")
	} else if parsed := parseRelativeURL(t, got); parsed.Query().Get("index") != "physical:idx-123" {
		t.Fatalf("expected dashboard index physical:idx-123, got %q", parsed.Query().Get("index"))
	}
}

func TestGetLogsHandler_ForwardsLimitWhenProvided(t *testing.T) {
	tests := []struct {
		name          string
		limit         int
		expectedLimit string
	}{
		{
			name:          "forwards explicit limit",
			limit:         25,
			expectedLimit: "25",
		},
		{
			name:          "uses configured max when unset",
			limit:         0,
			expectedLimit: "5000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != constants.EndpointLogsQueryRange {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				if got := r.URL.Query().Get("limit"); got != tt.expectedLimit {
					t.Fatalf("expected limit %q, got %q", tt.expectedLimit, got)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			}))
			defer server.Close()

			handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery: []map[string]interface{}{
					{
						"type": "filter",
						"query": map[string]interface{}{
							"$eq": []interface{}{"ServiceName", "api"},
						},
					},
				},
				LookbackMinutes: 5,
				Limit:           tt.limit,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
		})
	}
}

func TestGetLogsHandler_OmitsSourceLinkWhenIndexCannotBeResolved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("index"); got != "physical_index:missing" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
		case "/logs_settings/physical_indexes":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"properties":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$contains": []interface{}{"Body", "timeout"},
				},
			},
		},
		LookbackMinutes: 5,
		Index:           "physical_index:missing",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := referenceURL(t, result); got != "" {
		t.Fatalf("expected no reference_url when index resolution fails, got %q", got)
	}
}

func TestGetLogAttributesHandler_ForwardsIndex(t *testing.T) {
	tests := []struct {
		name            string
		lookbackMinutes int
		expectedPath    string
		response        string
	}{
		{
			name:            "labels api for longer lookbacks",
			lookbackMinutes: 30,
			expectedPath:    "/logs/api/v1/labels",
			response:        `{"status":"success","data":["service","severity"]}`,
		},
		{
			name:            "series api for short lookbacks",
			lookbackMinutes: 15,
			expectedPath:    "/logs/api/v2/series/json",
			response:        `{"status":"success","data":[{"service":"api","severity":"error"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.expectedPath {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				if got := r.URL.Query().Get("index"); got != "rehydration_index:block-a" {
					t.Fatalf("unexpected index %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			handler := NewGetLogAttributesHandler(server.Client(), testLogsConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesArgs{
				LookbackMinutes: tt.lookbackMinutes,
				Index:           "rehydration_index:block-a",
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
		})
	}
}

func TestGetServiceLogsHandler_ExplicitIndexOverridesFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("index"); got != "physical_index:payments" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			if got := r.URL.Query().Get("index_type"); got != "" {
				t.Fatalf("expected logs query index_type to be omitted, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(serviceLogsAPIResponse("timeout while calling db")))
		case "/logs_settings/physical_indexes":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"properties":[{"id":"idx-123","name":"payments"}]}`))
		case constants.EndpointPromQueryInstant:
			t.Fatal("did not expect fallback physical index lookup when index is explicitly provided")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		Service:         "api",
		LookbackMinutes: 5,
		Index:           "physical_index:payments",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if parsed := parseRelativeURL(t, referenceURL(t, result)); parsed.Query().Get("index") != "physical:idx-123" {
		t.Fatalf("expected dashboard index physical:idx-123, got %q", parsed.Query().Get("index"))
	}
}

func TestGetServiceLogsHandler_OmitsIndexWhenNotProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("index"); got != "" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(serviceLogsAPIResponse("no-index path worked")))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		Service:         "api",
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := parseRelativeURL(t, referenceURL(t, result)).Query().Get("index"); got != "" {
		t.Fatalf("expected dashboard index to be omitted, got %q", got)
	}
}

func TestGetServiceLogsHandler_ForwardsLargeLimitWhenProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("limit"); got != "2500" {
				t.Fatalf("expected limit %q, got %q", "2500", got)
			}
			if got := r.URL.Query().Get("index"); got != "physical_index:payments" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(serviceLogsAPIResponse("large limit forwarded")))
		case "/logs_settings/physical_indexes":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"properties":[{"id":"idx-123","name":"payments"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		Service:         "api",
		LookbackMinutes: 5,
		Limit:           2500,
		Index:           "physical_index:payments",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestGetServiceLogsHandler_UsesFrontendParityFilters(t *testing.T) {
	expectedQuery := buildServiceLogsQuery(
		"l9alert-example",
		[]string{"error", "fatal", "critical"},
		nil,
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("limit"); got == "" || got == "0" {
				t.Fatalf("expected a positive chunk limit, got %q", got)
			}
			if got := r.URL.Query().Get("index"); got != "physical_index:payments" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			if got := r.URL.Query().Get("index_type"); got != "" {
				t.Fatalf("expected logs query index_type to be omitted, got %q", got)
			}

			var body struct {
				Pipeline []map[string]interface{} `json:"pipeline"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}
			if !reflect.DeepEqual(body.Pipeline, expectedQuery) {
				gotJSON, _ := json.Marshal(body.Pipeline)
				wantJSON, _ := json.Marshal(expectedQuery)
				t.Fatalf("unexpected service logs pipeline\nwant: %s\ngot:  %s", wantJSON, gotJSON)
			}

			rawBody, _ := json.Marshal(body.Pipeline)
			if strings.Contains(string(rawBody), "$regex") || strings.Contains(string(rawBody), "(?i)") {
				t.Fatalf("expected frontend-parity operators without regex flags, got %s", rawBody)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(serviceLogsAPIResponse("severity filters matched")))
		case "/logs_settings/physical_indexes":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"properties":[{"id":"idx-123","name":"payments"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		Service:         "l9alert-example",
		StartTimeISO:    "2026-03-31T07:16:38.000Z",
		EndTimeISO:      "2026-04-01T07:16:38.907Z",
		Limit:           100,
		SeverityFilters: []string{"error", "fatal", "critical"},
		Index:           "physical_index:payments",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestGetServiceLogsHandler_AppliesEnvFilterToFetchAndDeepLink(t *testing.T) {
	expectedQuery := addServiceLogsEnvFilter(
		buildServiceLogsQuery("api", []string{"error"}, []string{"timeout"}),
		"production",
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointLogsQueryRange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		var body struct {
			Pipeline []map[string]interface{} `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if !reflect.DeepEqual(body.Pipeline, expectedQuery) {
			gotJSON, _ := json.Marshal(body.Pipeline)
			wantJSON, _ := json.Marshal(expectedQuery)
			t.Fatalf("unexpected service logs pipeline\nwant: %s\ngot:  %s", wantJSON, gotJSON)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(serviceLogsAPIResponse("env filter matched")))
	}))
	defer server.Close()

	handler := NewGetServiceLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		Service:         "api",
		LookbackMinutes: 5,
		SeverityFilters: []string{"error"},
		BodyFilters:     []string{"timeout"},
		Env:             "production",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	rawPipeline := parseRelativeURL(t, referenceURL(t, result)).Query().Get("pipeline")
	if rawPipeline == "" {
		t.Fatal("expected dashboard pipeline in reference_url")
	}

	var dashboardPipeline []map[string]interface{}
	if err := json.Unmarshal([]byte(rawPipeline), &dashboardPipeline); err != nil {
		t.Fatalf("failed to decode dashboard pipeline %q: %v", rawPipeline, err)
	}
	if !reflect.DeepEqual(dashboardPipeline, expectedQuery) {
		gotJSON, _ := json.Marshal(dashboardPipeline)
		wantJSON, _ := json.Marshal(expectedQuery)
		t.Fatalf("unexpected dashboard pipeline\nwant: %s\ngot:  %s", wantJSON, gotJSON)
	}
}

func testLogsConfig(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		Region:     "us-east-1",
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func referenceURL(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	if result.Meta == nil {
		return ""
	}

	raw, ok := result.Meta["reference_url"]
	if !ok || raw == nil {
		return ""
	}

	value, ok := raw.(string)
	if !ok {
		t.Fatalf("reference_url has unexpected type %T", raw)
	}

	return value
}

func parseRelativeURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("failed to parse url %q: %v", raw, err)
	}

	return parsed
}

func serviceLogsAPIResponse(message string) string {
	return `{"data":{"result":[{"stream":{"severity":"ERROR"},"values":[["1741500000000000000","` + strings.ReplaceAll(message, `"`, `\"`) + `"]]}]}}`
}

func TestGetLoggingServicesHandler_ReturnsEntries(t *testing.T) {
	promResp := `[
		{"metric":{"name":"payments","service_name":"checkout","env":"production","severity":"error"},"value":[1700000000,"3"]},
		{"metric":{"name":"default","service_name":"api","env":"staging","severity":"info"},"value":[1700000000,"10"]}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointPromQueryInstant {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(promResp))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var entries []LoggingServiceEntry
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &entries); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	byService := make(map[string]LoggingServiceEntry, len(entries))
	for _, e := range entries {
		byService[e.ServiceName] = e
	}

	checkout := byService["checkout"]
	if checkout.PhysicalIndex != "physical_index:payments" {
		t.Errorf("expected physical_index:payments for checkout, got %q", checkout.PhysicalIndex)
	}
	if checkout.Env != "production" {
		t.Errorf("expected env production for checkout, got %q", checkout.Env)
	}
	if checkout.Severity != "error" {
		t.Errorf("expected severity error for checkout, got %q", checkout.Severity)
	}

	api := byService["api"]
	if api.PhysicalIndex != "physical_index:default" {
		t.Errorf("expected physical_index:default for api, got %q", api.PhysicalIndex)
	}
}

func TestGetLoggingServicesHandler_FiltersQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointPromQueryInstant {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if !strings.Contains(body.Query, `service_name="checkout"`) {
			t.Errorf("expected service_name filter in query, got %q", body.Query)
		}
		if !strings.Contains(body.Query, `env="production"`) {
			t.Errorf("expected env filter in query, got %q", body.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{
		Service: "checkout",
		Env:     "production",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestGetLoggingServicesHandler_ServiceOnlyFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if !strings.Contains(body.Query, `service_name="api"`) {
			t.Errorf("expected service_name filter, got %q", body.Query)
		}
		if strings.Contains(body.Query, "env=") {
			t.Errorf("unexpected env filter in query, got %q", body.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{Service: "api"})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestGetLoggingServicesHandler_EnvOnlyFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if !strings.Contains(body.Query, `env="production"`) {
			t.Errorf("expected env filter, got %q", body.Query)
		}
		if strings.Contains(body.Query, "service_name=") {
			t.Errorf("unexpected service_name filter, got %q", body.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{Env: "production"})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestGetLoggingServicesHandler_NoFilterQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if body.Query != "physical_index_service_count" {
			t.Errorf("expected bare metric query, got %q", body.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var entries []LoggingServiceEntry
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &entries); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty entries for empty prom response, got %d", len(entries))
	}
}

func TestGetLoggingServicesHandler_MissingNameDefaultsToDefault(t *testing.T) {
	promResp := `[{"metric":{"service_name":"worker","env":"production","severity":"warn"},"value":[1700000000,"1"]}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(promResp))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var entries []LoggingServiceEntry
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &entries); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].PhysicalIndex != "physical_index:default" {
		t.Errorf("missing name label should default to physical_index:default, got %q", entries[0].PhysicalIndex)
	}
}

func TestGetLoggingServicesHandler_PromError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	handler := NewGetLoggingServicesHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLoggingServicesArgs{})
	if err == nil {
		t.Fatal("expected error on prom 500, got nil")
	}
}
