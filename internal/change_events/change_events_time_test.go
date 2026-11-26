package change_events

import (
	"context"
	"encoding/json"
	"io"
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
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetChangeEventsHandler_InvalidTimeOrder(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO: "2026-02-09T16:04:05Z",
		EndTimeISO:   "2026-02-09T15:04:05Z",
	})
	if err == nil {
		t.Fatalf("expected error for inverted time range, got nil")
	}
	if !strings.Contains(err.Error(), "start_time cannot be after end_time") {
		t.Fatalf("expected time-order error, got: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no upstream requests on validation failure, got %d", requestCount)
	}
}

func TestGetChangeEventsHandler_ExplicitRangePrecedence(t *testing.T) {
	type promReq struct {
		Timestamp int64  `json:"timestamp"`
		Window    int64  `json:"window"`
		Query     string `json:"query"`
	}
	captured := make([]promReq, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		var reqBody promReq
		_ = json.Unmarshal(body, &reqBody)
		captured = append(captured, reqBody)

		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		switch r.URL.Path {
		case constants.EndpointPromLabelValues:
			_, _ = io.WriteString(w, `["deployment"]`)
		case constants.EndpointPromQueryInstant:
			_, _ = io.WriteString(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO:    "2026-02-09T15:04:05Z",
		EndTimeISO:      "2026-02-09T16:04:05Z",
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected one unfiltered sample request, got %d", len(captured))
	}

	// endTimeParam = "2026-02-09T16:04:05Z" = 1770653045
	for _, req := range captured {
		if req.Timestamp != 1770653045 {
			t.Fatalf("timestamp = %d, want %d (= endTimeParam)", req.Timestamp, int64(1770653045))
		}
		if !strings.HasSuffix(req.Query, "[3601s]") {
			t.Fatalf("query = %q, want a 3601-second range vector", req.Query)
		}
	}
}

func TestFractionalEndKeepsReturnedEventNameDiscoverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		switch r.URL.Path {
		case constants.EndpointPromLabelValues:
			_, _ = io.WriteString(w, `[]`)
		case constants.EndpointPromQueryInstant:
			_, _ = io.WriteString(w, `[{"metric":{"event_name":"deployment"},"values":[[1770649445.5,"1"]]}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO: "2026-02-09T15:04:05.125Z",
		EndTimeISO:   "2026-02-09T15:04:05.875Z",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var response struct {
		AvailableEventNames []string     `json:"available_event_names"`
		ChangeEvents        []TimeSeries `json:"change_events"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !reflect.DeepEqual(response.AvailableEventNames, []string{"deployment"}) || len(response.ChangeEvents) != 1 {
		t.Fatalf("available=%v events=%d, want deployment discoverable with one event", response.AvailableEventNames, len(response.ChangeEvents))
	}
}

// Filtering on the MCP spellings alone returns zero series even when matching
// events exist.
func TestGetChangeEventsHandler_FiltersUseStoredMetricLabels(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Query != "" {
			capturedQuery = req.Query
		}
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		ServiceName:     "checkout",
		Env:             "prod",
		EventName:       "deployment",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	for _, want := range []string{
		`service="checkout"`,
		`deployment_environment="prod"`,
		`event_name="deployment"`,
	} {
		if !strings.Contains(capturedQuery, want) {
			t.Fatalf("expected %s in PromQL (stored metric labels), got: %s", want, capturedQuery)
		}
	}
}

// event_name is stamped on every series, so an event_type value could never be
// matched by a filter.
func TestGetChangeEventsHandler_DiscoveryReadsUnfilteredEventSamples(t *testing.T) {
	var capturedQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)
		capturedQueries = append(capturedQueries, req.Query)
		_, _ = io.WriteString(w, `[{"metric":{"event_name":"deployment","event_type":"rollback"},"values":[[1770653045,"1"]]}]`)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO: "2026-02-09T15:04:05Z",
		EndTimeISO:   "2026-02-09T16:04:05Z",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := utils.GetTextContent(t, result)
	var response struct {
		AvailableEventNames []string `json:"available_event_names"`
	}
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := strings.Join(response.AvailableEventNames, ",")
	if got != "deployment" {
		t.Fatalf("available_event_names = %q, want deployment (event_type must not be unioned)", got)
	}

	// Discovery must carry the query's exclusions, or it offers names that
	// every filtered call then rejects.
	joined := strings.Join(capturedQueries, " ")
	for _, want := range []string{excludeBackupEvents, excludeScheduledSearchEvents} {
		if !strings.Contains(joined, want) {
			t.Fatalf("event_name discovery must apply %s, got queries %v", want, capturedQueries)
		}
	}
}

func TestGetChangeEventsHandler_MetricsExplorerLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		switch r.URL.Path {
		case constants.EndpointPromLabelValues:
			_, _ = io.WriteString(w, `["deployment"]`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		OrgSlug:    "acme",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO: "2026-02-09T15:04:05Z",
		EndTimeISO:   "2026-02-09T16:04:05Z",
		ServiceName:  "checkout",
		Env:          "prod",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	raw, _ := res.Meta["reference_url"].(string)
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/v2/organizations/acme/metrics" {
		t.Fatalf("reference_url = %q, want the metrics explorer", raw)
	}
	q := u.Query()
	want := map[string]string{
		"from":    "1770649445",
		"to":      "1770653045",
		"cluster": "cluster-1",
		"mode":    "code",
		"query":   strings.Join(changeEventSelectors(GetChangeEventsArgs{ServiceName: "checkout", Env: "prod"}), " or "),
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}
