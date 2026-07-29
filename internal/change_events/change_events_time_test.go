package change_events

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestGetChangeEventsHandler_CancelsDiscoveryOnPrimaryFailure(t *testing.T) {
	discoveryStarted := make(chan struct{}, 1)
	releaseDiscovery := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromLabelValues:
			select {
			case discoveryStarted <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-releaseDiscovery:
			}
		case constants.EndpointPromQueryInstant:
			<-discoveryStarted
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	defer close(releaseDiscovery)

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	completed := make(chan error, 1)
	go func() {
		_, _, err := NewGetChangeEventsHandler(server.Client(), cfg)(
			context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{LookbackMinutes: 5},
		)
		completed <- err
	}()

	select {
	case err := <-completed:
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("error = %v, want primary query failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler waited for best-effort discovery after primary query failure")
	}
}

func TestGetChangeEventsHandler_ExplicitRangePrecedence(t *testing.T) {
	type promReq struct {
		Path      string `json:"-"`
		Timestamp int64  `json:"timestamp"`
		Window    int64  `json:"window"`
	}
	captured := make([]promReq, 0, 4)
	var capturedMutex sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		var reqBody promReq
		_ = json.Unmarshal(body, &reqBody)
		reqBody.Path = r.URL.Path
		capturedMutex.Lock()
		captured = append(captured, reqBody)
		capturedMutex.Unlock()

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

	if len(captured) != 4 {
		t.Fatalf("expected 4 upstream requests, got %d", len(captured))
	}

	// endTimeParam = "2026-02-09T16:04:05Z" = 1770653045
	labelRequests := 0
	instantRequests := 0
	for _, req := range captured {
		if req.Timestamp != 1770653045 {
			t.Fatalf("timestamp = %d, want %d (= endTimeParam)", req.Timestamp, int64(1770653045))
		}
		switch req.Path {
		case constants.EndpointPromLabelValues:
			labelRequests++
			if req.Window != 3600 {
				t.Fatalf("label window = %d, want %d", req.Window, int64(3600))
			}
		case constants.EndpointPromQueryInstant:
			instantRequests++
		}
	}
	if labelRequests != 3 || instantRequests != 1 {
		t.Fatalf("requests = %d label and %d instant, want 3 label and 1 instant", labelRequests, instantRequests)
	}
}

func TestBuildChangeEventsQueryUsesRawRangeVectorAndEscapesValues(t *testing.T) {
	query := buildChangeEventsQuery(GetChangeEventsArgs{
		ServiceName: `checkout"api`, Env: "prod", EventName: "deployment",
	}, 3600)

	if !strings.HasSuffix(query, `[3600s]`) {
		t.Fatalf("raw range vector missing: %s", query)
	}
	if strings.Contains(query, `event_name!~`) || strings.Contains(query, `l9_event_name!~`) {
		t.Fatalf("explicit event filter must not be contradicted by default exclusions: %s", query)
	}
	if !strings.Contains(query, `service_name="checkout\"api"`) {
		t.Fatalf("service matcher was not escaped: %s", query)
	}
}

func TestFilterChangeEventSeriesUsesCanonicalAliasPrecedence(t *testing.T) {
	series := []TimeSeries{
		{Metric: map[string]string{"event_name": "deploy", "event_type": "legacy"}},
		{Metric: map[string]string{"event_type": "deploy"}},
		{Metric: map[string]string{"l9_event_name": "deploy"}},
		{Metric: map[string]string{"event_name": "rollback", "event_type": "deploy"}},
		{Metric: map[string]string{"event_type": "manual_rehydration_event"}},
	}

	filtered := filterChangeEventSeries(series, "deploy")
	if len(filtered) != 3 {
		t.Fatalf("filtered series = %d, want 3", len(filtered))
	}
	if defaults := filterChangeEventSeries(series, ""); len(defaults) != 4 {
		t.Fatalf("default filtered series = %d, want 4", len(defaults))
	}
	if explicit := filterChangeEventSeries(series, "manual_rehydration_event"); len(explicit) != 1 {
		t.Fatalf("explicit excluded-name series = %d, want 1", len(explicit))
	}
}

// Verifies the renamed input fields (service_name, env) actually reach the
// backend PromQL as service_name="..."/env="..." — the rename is only correct
// if the handler wires the new fields into the query, not just the schema.
func TestGetChangeEventsHandler_FiltersUseCanonicalLabels(t *testing.T) {
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
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(capturedQuery, `service_name="checkout"`) {
		t.Fatalf("expected service_name=\"checkout\" filter in query, got: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, `env="prod"`) {
		t.Fatalf("expected env=\"prod\" filter in query, got: %s", capturedQuery)
	}
}

func TestGetChangeEventsHandlerCountsRawPointsAndToleratesDiscoveryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var request struct {
			Label string `json:"label"`
		}
		_ = json.Unmarshal(body, &request)

		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		switch r.URL.Path {
		case constants.EndpointPromLabelValues:
			if request.Label == "event_type" {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = io.WriteString(w, `["deployment","manual_rehydration_event"]`)
		case constants.EndpointPromQueryInstant:
			_, _ = io.WriteString(w, `[{"metric":{"event_name":"deployment"},"values":[[1770650000,"1"],[1770650060,"1"]]}]`)
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
	result, _, err := NewGetChangeEventsHandler(server.Client(), cfg)(
		context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{LookbackMinutes: 30},
	)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var response struct {
		AvailableEventNames []string `json:"available_event_names"`
		Count               int      `json:"count"`
		SeriesCount         int      `json:"series_count"`
		Warnings            []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Count != 2 || response.SeriesCount != 1 {
		t.Fatalf("counts = %d points/%d series, want 2/1", response.Count, response.SeriesCount)
	}
	if len(response.AvailableEventNames) != 1 || response.AvailableEventNames[0] != "deployment" {
		t.Fatalf("available_event_names = %#v, want [deployment]", response.AvailableEventNames)
	}
	if len(response.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one discovery warning", response.Warnings)
	}
}
