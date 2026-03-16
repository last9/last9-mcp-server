package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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
			expectedLimit: "50000",
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
			if got := r.URL.Query().Get("index_type"); got != "physical" {
				t.Fatalf("unexpected logs query index_type %q", got)
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

func TestGetServiceLogsHandler_FallsBackToFetchedPhysicalIndex(t *testing.T) {
	promLookupCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQueryInstant:
			promLookupCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"metric":{"name":"payments"},"value":[1,"1"]}]`))
		case constants.EndpointLogsQueryRange:
			if got := r.URL.Query().Get("index"); got != "physical_index:payments" {
				t.Fatalf("unexpected logs query index %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(serviceLogsAPIResponse("fallback path worked")))
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
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !promLookupCalled {
		t.Fatal("expected fallback physical index lookup when index is omitted")
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
