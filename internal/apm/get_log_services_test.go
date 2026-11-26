package apm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type logServiceSeries struct {
	service, env, index string
	lastSeen            int64
}

type logServicesQuery struct {
	Query     string `json:"query"`
	Timestamp int64  `json:"timestamp"`
}

type fakeLogServicesAPI struct {
	mu      sync.Mutex
	queries []logServicesQuery
}

func (f *fakeLogServicesAPI) recorded() []logServicesQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]logServicesQuery(nil), f.queries...)
}

// newLogServicesServer answers the nth instant query with replies[n] (the last reply repeats).
func newLogServicesServer(t *testing.T, replies ...func(w http.ResponseWriter)) (*fakeLogServicesAPI, models.Config, *http.Client) {
	t.Helper()
	f := &fakeLogServicesAPI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/prom_query_instant") {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var q logServicesQuery
		_ = json.Unmarshal(raw, &q)
		f.mu.Lock()
		n := len(f.queries)
		f.queries = append(f.queries, q)
		f.mu.Unlock()
		replies[min(n, len(replies)-1)](w)
	}))
	t.Cleanup(server.Close)
	cfg := models.Config{
		APIBaseURL:   server.URL,
		Region:       "us-east-1",
		TokenManager: &auth.TokenManager{AccessToken: "mock-token", ExpiresAt: time.Now().Add(time.Hour)},
	}
	return f, cfg, server.Client()
}

func seriesReply(series ...logServiceSeries) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		out := make([]map[string]any, 0, len(series))
		for _, s := range series {
			out = append(out, map[string]any{
				"metric": map[string]string{"service_name": s.service, "env": s.env, "name": s.index},
				"value":  []any{1, fmt.Sprint(s.lastSeen)},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

type logServicesOutput struct {
	Rows []struct {
		Name       string   `json:"name"`
		Envs       []string `json:"envs"`
		Indexes    []string `json:"indexes"`
		LastSeen   string   `json:"last_seen"`
		Similarity *int     `json:"similarity"`
	} `json:"rows"`
	Total     int    `json:"total"`
	Returned  int    `json:"returned"`
	Truncated bool   `json:"truncated"`
	Note      string `json:"note"`
}

func callLogServices(t *testing.T, client *http.Client, cfg models.Config, args GetLogServicesArgs) (logServicesOutput, *mcp.CallToolResult) {
	t.Helper()
	res, _, err := NewGetLogServicesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	texts := resultTexts(res)
	if res.IsError {
		return logServicesOutput{}, res
	}
	var out logServicesOutput
	if err := json.Unmarshal([]byte(texts[0]), &out); err != nil {
		t.Fatalf("decode %q: %v", texts[0], err)
	}
	return out, res
}

func intPtr(n int) *int { return &n }

func TestGetLogServicesGroupsSeriesByServiceName(t *testing.T) {
	f, cfg, client := newLogServicesServer(t, seriesReply(
		logServiceSeries{"svc-logs-only", "prod", "default", 1700000000},
		logServiceSeries{"svc-logs-only", "staging", "archive", 1700000600},
		logServiceSeries{"svc-logs-only", "prod", "archive", 1700000300},
		logServiceSeries{"svc-other", "prod", "default", 1700000100},
		logServiceSeries{"", "prod", "default", 1700000100},
	))

	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{LookbackMinutes: 30})

	if out.Total != 2 || out.Returned != 2 || out.Truncated {
		t.Fatalf("total/returned/truncated = %d/%d/%v, want 2/2/false", out.Total, out.Returned, out.Truncated)
	}
	first := out.Rows[0]
	if first.Name != "svc-logs-only" || strings.Join(first.Envs, ",") != "prod,staging" || strings.Join(first.Indexes, ",") != "archive,default" {
		t.Errorf("first row = %+v", first)
	}
	if want := time.Unix(1700000600, 0).UTC().Format(time.RFC3339); first.LastSeen != want {
		t.Errorf("last_seen = %q, want %q", first.LastSeen, want)
	}
	if first.Similarity != nil {
		t.Errorf("similarity set without a query: %d", *first.Similarity)
	}
	q := f.recorded()
	if len(q) != 1 {
		t.Fatalf("queries = %d, want 1", len(q))
	}
	want := `max by (service_name, env, name) (tlast_over_time(physical_index_service_count{destination="logs"}[1800s]))`
	if q[0].Query != want {
		t.Errorf("query = %s\nwant    %s", q[0].Query, want)
	}
}

func TestGetLogServicesFiltersByEnvAndIndex(t *testing.T) {
	f, cfg, client := newLogServicesServer(t, seriesReply(logServiceSeries{"svc-a", "prod", "archive", 1700000000}))

	callLogServices(t, client, cfg, GetLogServicesArgs{Env: "prod", Index: "archive", LookbackMinutes: 5})

	want := `physical_index_service_count{destination="logs",env="prod",name="archive"}[300s]`
	if q := f.recorded(); !strings.Contains(q[0].Query, want) {
		t.Errorf("query = %s, want it to contain %s", q[0].Query, want)
	}
}

func TestGetLogServicesTotalCountsEveryNameWhenLimitCutsRows(t *testing.T) {
	var series []logServiceSeries
	for i := range 12 {
		series = append(series, logServiceSeries{fmt.Sprintf("svc-%02d", i), "prod", "default", 1700000000})
	}
	_, cfg, client := newLogServicesServer(t, seriesReply(series...))

	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{LookbackMinutes: 5})
	if out.Total != 12 || out.Returned != 10 || !out.Truncated || len(out.Rows) != 10 {
		t.Errorf("default limit: total/returned/truncated/rows = %d/%d/%v/%d, want 12/10/true/10", out.Total, out.Returned, out.Truncated, len(out.Rows))
	}

	out, _ = callLogServices(t, client, cfg, GetLogServicesArgs{LookbackMinutes: 5, Limit: intPtr(12)})
	if out.Total != 12 || out.Returned != 12 || out.Truncated {
		t.Errorf("limit 12: total/returned/truncated = %d/%d/%v, want 12/12/false", out.Total, out.Returned, out.Truncated)
	}
}

func TestGetLogServicesRanksReorderedNameFirst(t *testing.T) {
	_, cfg, client := newLogServicesServer(t, seriesReply(
		logServiceSeries{"alpha-beta-gamme", "prod", "default", 1700000000},
		logServiceSeries{"zeta-service", "prod", "default", 1700000000},
		logServiceSeries{"beta-alpha-gamma", "prod", "default", 1700000000},
		logServiceSeries{"alpha-beta", "prod", "default", 1700000000},
	))

	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{Query: "alpha-beta-gamma", LookbackMinutes: 5})

	var got []string
	for _, r := range out.Rows {
		got = append(got, fmt.Sprintf("%s=%d", r.Name, *r.Similarity))
	}
	want := "beta-alpha-gamma=100,alpha-beta=67,alpha-beta-gamme=67,zeta-service=0"
	if strings.Join(got, ",") != want {
		t.Errorf("ranking = %s, want %s", strings.Join(got, ","), want)
	}
}

func TestGetLogServicesRetriesEmptyDefaultWindowOverFourDays(t *testing.T) {
	f, cfg, client := newLogServicesServer(t, seriesReply(), seriesReply(logServiceSeries{"svc-sparse", "prod", "default", 1700000000}))

	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{})

	q := f.recorded()
	if len(q) != 2 || !strings.Contains(q[0].Query, "[3600s]") || !strings.Contains(q[1].Query, "[345600s]") {
		t.Fatalf("queries = %+v, want 60 minutes then 4 days", q)
	}
	if out.Total != 1 || !strings.Contains(out.Note, "4 days") {
		t.Errorf("total = %d, note = %q, want 1 and a note naming the 4-day retry", out.Total, out.Note)
	}
}

func TestGetLogServicesNeverWidensExplicitWindow(t *testing.T) {
	f, cfg, client := newLogServicesServer(t, seriesReply())

	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{LookbackMinutes: 15})

	if n := len(f.recorded()); n != 1 {
		t.Errorf("queries = %d, want 1", n)
	}
	if out.Total != 0 || out.Rows == nil || !strings.Contains(out.Note, "not proof") {
		t.Errorf("out = %+v, want empty rows [] and a note that absence is not proof", out)
	}
}

func TestGetLogServicesReportsFailedRetryAsIncomplete(t *testing.T) {
	_, cfg, client := newLogServicesServer(t, seriesReply(), func(w http.ResponseWriter) {
		http.Error(w, "boom", http.StatusBadGateway)
	})

	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{})

	if !strings.Contains(out.Note, "incomplete") {
		t.Errorf("note = %q, want discovery reported incomplete", out.Note)
	}
}

func TestGetLogServicesCancelledRetryReturnsCancellation(t *testing.T) {
	retryStarted := make(chan struct{})
	releaseRetry := make(chan struct{})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			seriesReply()(w)
			return
		}
		close(retryStarted)
		select {
		case <-r.Context().Done():
		case <-releaseRetry:
		}
	}))
	t.Cleanup(server.Close)
	cfg := testSummaryConfig(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := NewGetLogServicesHandler(server.Client(), cfg)(ctx, &mcp.CallToolRequest{}, GetLogServicesArgs{})
		done <- err
	}()

	<-retryStarted
	cancel()
	err := <-done
	close(releaseRetry)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGetLogServicesUpstreamErrorIsToolError(t *testing.T) {
	_, cfg, client := newLogServicesServer(t, func(w http.ResponseWriter) {
		http.Error(w, "bad", http.StatusBadRequest)
	})

	_, res := callLogServices(t, client, cfg, GetLogServicesArgs{LookbackMinutes: 5})

	if !res.IsError {
		t.Errorf("want a tool error, got %v", resultTexts(res))
	}
}

func TestGetLogServicesReturnsLogOnlyServiceMissingFromServiceSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body logServicesQuery
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Query, "physical_index_service_count") {
			seriesReply(logServiceSeries{"svc-logs-only", "prod", "default", 1700000000})(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, promInstantJSON(t, []summaryPromSample{{service: "svc-apm", env: "prod", value: "5"}}))
	}))
	t.Cleanup(server.Close)
	cfg := testSummaryConfig(server.URL)

	out, _ := callLogServices(t, server.Client(), cfg, GetLogServicesArgs{LookbackMinutes: 5})
	if len(out.Rows) != 1 || out.Rows[0].Name != "svc-logs-only" {
		t.Errorf("get_log_services rows = %+v, want svc-logs-only", out.Rows)
	}

	summary, _, err := NewServiceSummaryHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, ServiceSummaryArgs{LookbackMinutes: 5})
	if err != nil {
		t.Fatalf("get_service_summary: %v", err)
	}
	text := strings.Join(resultTexts(summary), "\n")
	if !strings.Contains(text, "svc-apm") || strings.Contains(text, "svc-logs-only") {
		t.Errorf("get_service_summary = %s, want svc-apm and no svc-logs-only", text)
	}
}

func TestGetLogServicesRejectsOutOfRangeLimitBeforeQuerying(t *testing.T) {
	for _, limit := range []int{0, -1, 101} {
		f, cfg, client := newLogServicesServer(t, seriesReply())
		_, _, err := NewGetLogServicesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, GetLogServicesArgs{Limit: intPtr(limit)})
		if err == nil || !strings.Contains(err.Error(), "limit") {
			t.Errorf("limit %d: err = %v, want a limit error", limit, err)
		}
		if n := len(f.recorded()); n != 0 {
			t.Errorf("limit %d: %d queries sent, want 0", limit, n)
		}
	}
	_, cfg, client := newLogServicesServer(t, seriesReply())
	out, _ := callLogServices(t, client, cfg, GetLogServicesArgs{LookbackMinutes: 5, Limit: intPtr(100)})
	if out.Rows == nil {
		t.Errorf("limit 100 rejected")
	}
}

func TestGetLogServicesExplicitWindowMustHavePositiveDuration(t *testing.T) {
	const start = "2026-11-25T02:00:00Z"

	f, cfg, client := newLogServicesServer(t, seriesReply())
	_, _, err := NewGetLogServicesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, GetLogServicesArgs{
		StartTimeISO: start,
		EndTimeISO:   start,
	})
	if err == nil || !strings.Contains(err.Error(), "end_time_iso must be after start_time_iso") {
		t.Errorf("err = %v, want a positive-duration window error", err)
	}
	if n := len(f.recorded()); n != 0 {
		t.Errorf("queries = %d, want 0", n)
	}

	f, cfg, client = newLogServicesServer(t, seriesReply())
	callLogServices(t, client, cfg, GetLogServicesArgs{
		StartTimeISO: start,
		EndTimeISO:   "2026-11-25T02:01:00Z",
	})
	if n := len(f.recorded()); n != 1 {
		t.Errorf("one-minute control sent %d queries, want 1", n)
	}
}
