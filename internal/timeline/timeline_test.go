package timeline

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResolveTimelineRequestExplicitRangeTakesPrecedence(t *testing.T) {
	request, err := resolveTimelineRequest(GetChangeTimelineArgs{
		StartTimeISO: "2026-07-28T09:30:00Z", EndTimeISO: "2026-07-28T10:30:00Z",
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("resolveTimelineRequest() error = %v", err)
	}
	if request.End.Sub(request.Start) != time.Hour {
		t.Fatalf("duration = %s, want 1h", request.End.Sub(request.Start))
	}
	if request.Limit != defaultMaxEvents || len(request.Kinds) != 2 {
		t.Fatalf("defaults = limit %d, kinds %v", request.Limit, request.Kinds)
	}
}

func TestGetChangeTimelineInputSchemaBounds(t *testing.T) {
	schema := GetChangeTimelineInputSchema()
	properties := schema["properties"].(map[string]interface{})
	lookback := properties["lookback_minutes"].(map[string]interface{})
	maximum := lookback["maximum"].(float64)
	if maximum != maxLookbackMinutes {
		t.Fatalf("lookback maximum = %v, want %d", maximum, maxLookbackMinutes)
	}
	limit := properties["max_events"].(map[string]interface{})
	if limit["maximum"].(float64) != maxEvents {
		t.Fatalf("max_events maximum = %v, want %d", limit["maximum"], maxEvents)
	}
	dependent := schema["dependentRequired"].(map[string]interface{})
	if len(dependent) != 2 {
		t.Fatalf("dependentRequired = %#v, want both explicit bounds", dependent)
	}
}

func TestResolveTimelineRequestValidation(t *testing.T) {
	tests := []struct {
		name string
		args GetChangeTimelineArgs
		want string
	}{
		{name: "one explicit bound", args: GetChangeTimelineArgs{StartTimeISO: "2026-07-28T09:30:00Z"}, want: "must be supplied together"},
		{name: "long range", args: GetChangeTimelineArgs{StartTimeISO: "2026-07-28T09:00:00Z", EndTimeISO: "2026-07-28T10:30:00Z"}, want: "at most 60 minutes"},
		{name: "long lookback", args: GetChangeTimelineArgs{LookbackMinutes: 61}, want: "lookback_minutes"},
		{name: "invalid kind", args: GetChangeTimelineArgs{Kinds: []string{"deviation"}}, want: "kinds must contain"},
		{name: "large limit", args: GetChangeTimelineArgs{MaxEvents: 501}, want: "max_events"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveTimelineRequest(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestGetChangeTimelineHandlerForwardsContractAndAddsFollowUps(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, timelineFixture)
	}))
	defer server.Close()

	cfg := timelineTestConfig(server.URL)
	cfg.DatasourceName = "production"
	handler := NewGetChangeTimelineHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeTimelineArgs{
		StartTimeISO: " 2026-07-28T09:30:00Z ", EndTimeISO: " 2026-07-28T10:30:00Z ",
		ServiceName: " checkout-api ", Env: " production ", RuleID: " rule-1 ",
		Kinds: []string{kindChangeEvent, kindAlertEpisode}, MaxEvents: 25,
	})
	if err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	assertTimelineQuery(t, captured)
	output := utils.GetTextContent(t, result)
	assertTimelineOutput(t, output)
	if !strings.Contains(output, `1753693200000000000`) || strings.Contains(output, `1.7536932e+18`) {
		t.Fatalf("large numeric source value was not preserved exactly: %s", output)
	}
	if referenceURL, ok := result.Meta["reference_url"].(string); !ok || !strings.Contains(referenceURL, "rule_id=rule-1") {
		t.Fatalf("reference_url = %#v, want scoped rule link", result.Meta["reference_url"])
	}
}

func TestGetChangeTimelineHandlerSanitizesUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "secret upstream response")
	}))
	defer server.Close()

	handler := NewGetChangeTimelineHandler(server.Client(), timelineTestConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeTimelineArgs{})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v, want sanitized status", err)
	}
	if strings.Contains(err.Error(), "secret upstream response") {
		t.Fatalf("error leaked upstream response: %v", err)
	}
}

func TestGetChangeTimelineHandlerSanitizesTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	apiBaseURL := server.URL
	server.Close()

	handler := NewGetChangeTimelineHandler(server.Client(), timelineTestConfig(apiBaseURL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeTimelineArgs{})
	if err == nil || !strings.Contains(err.Error(), "change timeline request failed") {
		t.Fatalf("error = %v, want sanitized transport failure", err)
	}
	if strings.Contains(err.Error(), apiBaseURL) || strings.Contains(err.Error(), "start_time") {
		t.Fatalf("transport error leaked request URL: %v", err)
	}
}

func TestGetChangeTimelineHandlerGatesRecommendedFollowUps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, timelineFixture)
	}))
	defer server.Close()

	cfg := timelineTestConfig(server.URL)
	cfg.AllowedTools = toolsets.Set{
		"get_change_timeline": {},
		"get_alert_config":    {},
	}
	result, _, err := NewGetChangeTimelineHandler(server.Client(), cfg)(
		context.Background(), &mcp.CallToolRequest{}, GetChangeTimelineArgs{
			RuleID: "rule-1", ServiceName: "checkout-api",
		},
	)
	if err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	var response struct {
		FollowUps []followUp `json:"recommended_follow_ups"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(response.FollowUps) != 1 || response.FollowUps[0].Tool != "get_alert_config" {
		t.Fatalf("follow-ups = %#v, want only get_alert_config", response.FollowUps)
	}
}

func TestTimelineMetaOmitsAlertLinkForChangeOnly(t *testing.T) {
	request, err := resolveTimelineRequest(GetChangeTimelineArgs{Kinds: []string{kindChangeEvent}})
	if err != nil {
		t.Fatalf("resolveTimelineRequest() error = %v", err)
	}
	if meta := timelineMeta(timelineTestConfig("unused"), request); meta != nil {
		t.Fatalf("timelineMeta() = %#v, want nil for change-only timeline", meta)
	}
}

func assertTimelineQuery(t *testing.T, request *http.Request) {
	t.Helper()
	if request == nil {
		t.Fatal("timeline endpoint was not called")
	}
	query := request.URL.Query()
	checks := map[string]string{
		"start_time": "2026-07-28T09:30:00Z", "end_time": "2026-07-28T10:30:00Z",
		"service_name": "checkout-api", "env": "production", "rule_id": "rule-1",
		"limit": "25", "data_source_name": "production",
	}
	for name, expected := range checks {
		if query.Get(name) != expected {
			t.Errorf("query[%s] = %q, want %q", name, query.Get(name), expected)
		}
	}
	if len(query["kind"]) != 2 {
		t.Fatalf("kind query = %v, want two values", query["kind"])
	}
}

func assertTimelineOutput(t *testing.T, output string) {
	t.Helper()
	var response struct {
		Events    []map[string]any `json:"events"`
		Coverage  map[string]any   `json:"coverage"`
		FollowUps []followUp       `json:"recommended_follow_ups"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(response.Events) != 1 || len(response.Coverage) != 2 {
		t.Fatalf("API contract not preserved: %#v", response)
	}
	wantTools := []string{"get_alert_config", "get_apm_service_deviations", "get_service_logs", "get_service_traces"}
	if len(response.FollowUps) != len(wantTools) {
		t.Fatalf("follow-ups = %#v", response.FollowUps)
	}
	for index, tool := range wantTools {
		if response.FollowUps[index].Tool != tool {
			t.Errorf("follow-up %d = %q, want %q", index, response.FollowUps[index].Tool, tool)
		}
	}
}

func timelineTestConfig(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		OrgSlug:    "test-org",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour),
		},
	}
}

const timelineFixture = `{
  "time_range":{"start":"2026-07-28T09:30:00Z","end":"2026-07-28T10:30:00Z"},
  "scope":{"service_name":"checkout-api","env":"production"},
  "events":[{"id":"change:1","kind":"change_event","occurred_at":"2026-07-28T10:00:00Z","details":{"source_sequence":1753693200000000000}}],
  "relationships":[],
  "coverage":{"change_events":{"status":"complete","count":1},"alerts":{"status":"complete","count":0}},
  "warnings":[]
}`
