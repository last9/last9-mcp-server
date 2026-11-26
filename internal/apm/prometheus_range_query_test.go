package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPromqlRangeQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, *cfg)

	args := PromqlRangeQueryArgs{
		Query:        "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details []TimeSeries
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestPromqlRangeHandler_UsesLookbackAndExplicitPrecedence(t *testing.T) {
	type capturedReq struct {
		Timestamp int64 `json:"timestamp"`
		Window    int64 `json:"window"`
	}

	var captured []capturedReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/prom_query") {
			t.Fatalf("expected prom_query endpoint, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var reqPayload capturedReq
		_ = json.Unmarshal(body, &reqPayload)
		captured = append(captured, reqPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for lookback mode: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(captured))
	}
	if captured[0].Window != 1800 {
		t.Fatalf("window = %d, want %d", captured[0].Window, int64(1800))
	}

	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO:    "2025-06-23T16:00:00Z",
		EndTimeISO:      "2025-06-23T16:10:00Z",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for explicit mode: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(captured))
	}
	if captured[1].Window != 600 {
		t.Fatalf("window with explicit timestamps = %d, want %d", captured[1].Window, int64(600))
	}
}

func TestPromqlRangeHandler_DatasourceOverride(t *testing.T) {
	type capturedReq struct {
		ReadURL  string `json:"read_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var (
		captured capturedReq
		hitCount int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://default.example.com/prom",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "staging-user", Password: "staging-pass", Region: "us-east-1"},
		},
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	// No datasource — default credentials should reach the server
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler error (default): %v", err)
	}
	if captured.ReadURL != "https://default.example.com/prom" {
		t.Errorf("default: read_url = %q, want default URL", captured.ReadURL)
	}

	// With datasource override — staging credentials should reach the server
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "staging",
	})
	if err != nil {
		t.Fatalf("handler error (staging): %v", err)
	}
	if captured.ReadURL != "https://staging.example.com/prom" {
		t.Errorf("staging: read_url = %q, want staging URL", captured.ReadURL)
	}
	if captured.Username != "staging-user" {
		t.Errorf("staging: username = %q, want staging-user", captured.Username)
	}
	if captured.Password != "staging-pass" {
		t.Errorf("staging: password = %q, want staging-pass", captured.Password)
	}

	// Unknown datasource — handler must return error before hitting the server
	beforeUnknown := atomic.LoadInt32(&hitCount)
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown datasource, got nil")
	}
	if after := atomic.LoadInt32(&hitCount); after != beforeUnknown {
		t.Fatalf("unknown datasource: server was contacted %d time(s), want 0", after-beforeUnknown)
	}
}

func TestPromqlRangeQueryRelays400AndDrains502(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
		forbid     string
	}{
		{
			name:       "400 includes parse body",
			status:     http.StatusBadRequest,
			body:       `{"error":"parse error: unexpected identifier \"foo\""}`,
			wantSubstr: "parse error",
		},
		{
			name:       "422 too many samples gives actionable metrics error",
			status:     http.StatusUnprocessableEntity,
			body:       `{"error":"Too many samples queried. Please try selecting a smaller time range."}`,
			wantSubstr: "METRICS_QUERY_TOO_MANY_SAMPLES",
			forbid:     "Upstream response",
		},
		{
			name:       "502 omits body",
			status:     http.StatusBadGateway,
			body:       `{"error":"gateway SECRET"}`,
			wantSubstr: "HTTP 502",
			forbid:     "SECRET",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			cfg := models.Config{
				APIBaseURL: server.URL,
				Region:     "us-east-1",
				TokenManager: &auth.TokenManager{
					AccessToken: "test-token",
					ExpiresAt:   time.Now().Add(24 * time.Hour),
				},
			}
			handler := NewPromqlRangeQueryHandler(server.Client(), cfg)
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
				Query:           "up",
				LookbackMinutes: 5,
			})
			if err != nil {
				t.Fatalf("handler returned Go error %v; want IsError result", err)
			}
			if result == nil || !result.IsError {
				t.Fatal("expected IsError result")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tt.wantSubstr) {
				t.Fatalf("error %q missing %q", text, tt.wantSubstr)
			}
			if tt.forbid != "" && strings.Contains(text, tt.forbid) {
				t.Fatalf("error leaked %q: %s", tt.forbid, text)
			}
		})
	}
}

func TestPromqlRangeQueryHandler_ExplicitStepSpacing(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, *cfg)
	end := time.Now().UTC().Truncate(time.Minute)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:        "vector(1)",
		StartTimeISO: end.Add(-10 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
		Step:         120,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("handler returned error result: %s", utils.GetTextContent(t, result))
	}

	var series []struct {
		Values [][]json.Number `json:"values"`
	}
	dec := json.NewDecoder(strings.NewReader(utils.GetTextContent(t, result)))
	dec.UseNumber()
	if err := dec.Decode(&series); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(series) != 1 || len(series[0].Values) < 2 {
		t.Fatalf("want one series with at least two samples, got %+v", series)
	}
	values := series[0].Values
	for i := 1; i < len(values); i++ {
		prev, _ := values[i-1][0].Float64()
		cur, _ := values[i][0].Float64()
		if cur-prev != 120 {
			t.Fatalf("samples %d and %d are %vs apart, want 120s: %v", i-1, i, cur-prev, values)
		}
	}
}

func newStepCaptureServer(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	return server, &bodies
}

func stepTestConfig(baseURL string) models.Config {
	return models.Config{
		APIBaseURL:   baseURL,
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}
}

func TestPromqlRangeHandler_ForwardsStep(t *testing.T) {
	cases := []struct {
		name     string
		step     int64
		wantStep any
	}{
		{name: "omitted or zero keeps backend default", step: 0, wantStep: nil},
		{name: "explicit step in seconds", step: 120, wantStep: float64(120)},
		{name: "step equal to the window", step: 600, wantStep: float64(600)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, bodies := newStepCaptureServer(t)
			handler := NewPromqlRangeQueryHandler(server.Client(), stepTestConfig(server.URL))

			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
				Query:        "vector(1)",
				StartTimeISO: "2025-06-23T16:00:00Z",
				EndTimeISO:   "2025-06-23T16:10:00Z",
				Step:         tt.step,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if len(*bodies) != 1 {
				t.Fatalf("upstream requests = %d, want 1", len(*bodies))
			}
			step, present := (*bodies)[0]["step"]
			if tt.wantStep == nil && present {
				t.Fatalf("step sent as %v; omitted or zero must leave it to the backend", step)
			}
			if tt.wantStep != nil && step != tt.wantStep {
				t.Fatalf("step = %v, want %v", step, tt.wantStep)
			}
		})
	}
}

func TestPromqlRangeHandler_RejectsOutOfRangeStepBeforeQuery(t *testing.T) {
	cases := []struct {
		name    string
		end     string
		step    int64
		wantErr string
	}{
		{name: "negative", end: "2025-06-23T16:10:00Z", step: -60, wantErr: "step must be a positive number of seconds"},
		{name: "larger than the window", end: "2025-06-23T16:10:00Z", step: 601, wantErr: "at most the 600s query window"},
		{name: "more than 11000 points", end: "2025-06-23T19:03:21Z", step: 1, wantErr: "set step to at least 2 seconds"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, bodies := newStepCaptureServer(t)
			handler := NewPromqlRangeQueryHandler(server.Client(), stepTestConfig(server.URL))

			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
				Query: "vector(1)", StartTimeISO: "2025-06-23T16:00:00Z", EndTimeISO: tt.end, Step: tt.step,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if len(*bodies) != 0 {
				t.Fatalf("upstream requests = %d, want 0", len(*bodies))
			}
		})
	}
}

func TestPromqlRangeHandler_AcceptsStepAt11000Points(t *testing.T) {
	server, bodies := newStepCaptureServer(t)
	handler := NewPromqlRangeQueryHandler(server.Client(), stepTestConfig(server.URL))

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "vector(1)", StartTimeISO: "2025-06-23T16:00:00Z", EndTimeISO: "2025-06-23T19:03:20Z", Step: 1,
	})
	if err != nil {
		t.Fatalf("a window of exactly 11000 steps must be accepted: %v", err)
	}
	if len(*bodies) != 1 || (*bodies)[0]["step"] != float64(1) {
		t.Fatalf("bodies = %v, want one request with step 1", *bodies)
	}
}

func callPromRangeQuery(t *testing.T, body string) *mcp.CallToolResult {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	result, _, err := NewPromqlRangeQueryHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func promSeries(n int, labelBytes int) string {
	series := make([]string, n)
	for i := range series {
		series[i] = fmt.Sprintf(`{"metric":{"__name__":"up","pad":"%s"},"values":[[1700000000,"1"]]}`, strings.Repeat("x", labelBytes))
	}
	return "[" + strings.Join(series, ",") + "]"
}

func TestPromRangeQueryKeepsBodyUnchanged(t *testing.T) {
	body := promSeries(2, 3)
	result := callPromRangeQuery(t, body)

	if len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != body {
		t.Fatalf("content = %#v, want the upstream body unchanged and no envelope block", result.Content)
	}
	if result.StructuredContent != nil {
		t.Fatalf("structuredContent = %#v, want the series sent once", result.StructuredContent)
	}
	if env := utils.GetResultEnvelope(t, result); !env.IsEmpty() {
		t.Fatalf("envelope = %+v, want empty", env)
	}
}

func TestPromRangeQueryEmptyResultHintsLabelDiscovery(t *testing.T) {
	result := callPromRangeQuery(t, `[]`)

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "prometheus_labels") || !strings.Contains(env.Hints[0], "prometheus_label_values") {
		t.Fatalf("hints = %v, want one pointing at label discovery", env.Hints)
	}
}

func TestPromRangeQueryTrimsOversizedResponse(t *testing.T) {
	result := callPromRangeQuery(t, promSeries(40, 4000))

	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > utils.MaxResultTextBytes {
		t.Fatalf("payload text is %d bytes, over budget", len(text))
	}
	var kept []map[string]any
	if err := json.Unmarshal([]byte(text), &kept); err != nil {
		t.Fatalf("trimmed text is not a JSON array: %v", err)
	}
	if env := utils.GetResultEnvelope(t, result); len(kept) == 0 || env.TrimmedItems != 40-len(kept) {
		t.Fatalf("kept %d, trimmed_items %d, want the two to sum to 40", len(kept), env.TrimmedItems)
	}
}

func TestPromRangeQueryPassesThroughNonArrayBody(t *testing.T) {
	result := callPromRangeQuery(t, `not json`)

	if len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != "not json" {
		t.Fatalf("content = %#v, want the body unchanged", result.Content)
	}
	if result.StructuredContent != nil {
		t.Fatalf("structuredContent = %#v, want none for an unparseable body", result.StructuredContent)
	}
}
