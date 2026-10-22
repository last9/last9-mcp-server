package apm

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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type labelLookupRequest struct {
	Path   string
	Window int64
}

// labelLookupServer answers each call with the next body in bodies (the last one repeats)
// and records the path and window of every request.
func labelLookupServer(t *testing.T, bodies ...string) (models.Config, *http.Client, *[]labelLookupRequest) {
	t.Helper()
	var got []labelLookupRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Window int64 `json:"window"`
		}
		_ = json.Unmarshal(raw, &payload)
		got = append(got, labelLookupRequest{Path: r.URL.Path, Window: payload.Window})
		body := bodies[min(len(got), len(bodies))-1]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	cfg := models.Config{APIBaseURL: srv.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}
	return cfg, srv.Client(), &got
}

func resultTexts(t *testing.T, result *mcp.CallToolResult) []string {
	t.Helper()
	if result == nil {
		t.Fatal("nil result")
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result.Content)
	}
	var texts []string
	for _, c := range result.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content %T, want *mcp.TextContent", c)
		}
		texts = append(texts, tc.Text)
	}
	return texts
}

func windows(reqs []labelLookupRequest) []int64 {
	var out []int64
	for _, r := range reqs {
		out = append(out, r.Window)
	}
	return out
}

const (
	defaultWindowSeconds = 60 * 60
	sparseWindowSeconds  = 4 * 24 * 60 * 60
)

func TestPromqlLabelValues_ScopedEmptyRetriesWiderWithNote(t *testing.T) {
	cfg, client, got := labelLookupServer(t, `[]`, `["bucket-a","bucket-b"]`)

	result, _, err := NewPromqlLabelValuesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery: `aws_s3_bucketsizebytes{Namespace="AWS/S3"}`,
		Label:      "BucketName",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if w := windows(*got); len(w) != 2 || w[0] != defaultWindowSeconds || w[1] != sparseWindowSeconds {
		t.Fatalf("windows = %v, want [%d %d]", w, defaultWindowSeconds, sparseWindowSeconds)
	}
	texts := resultTexts(t, result)
	if len(texts) != 2 {
		t.Fatalf("content blocks = %d, want 2 (values, note): %q", len(texts), texts)
	}
	if texts[0] != `["bucket-a","bucket-b"]` {
		t.Fatalf("values = %s, want the wider window's", texts[0])
	}
	want := "No label values in the last 60 minutes; found 2 over the last 4 days (metric reports sparsely). Showing the wider window."
	if texts[1] != want {
		t.Fatalf("note = %q, want %q", texts[1], want)
	}
}

// The labels API wraps its list as {"labels":[...]}; these tests stub that live shape.
func TestPromqlLabels_ScopedEmptyRetriesWiderWithNote(t *testing.T) {
	cfg, client, got := labelLookupServer(t, `{"labels":[]}`, `{"labels":["__name__","BucketName","StorageType"]}`)

	result, _, err := NewPromqlLabelsHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelsArgs{
		MatchQuery: `{__name__="aws_s3_bucketsizebytes"}`,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if w := windows(*got); len(w) != 2 || w[1] != sparseWindowSeconds {
		t.Fatalf("windows = %v, want a retry over %d", w, sparseWindowSeconds)
	}
	texts := resultTexts(t, result)
	if len(texts) != 2 || texts[0] != `{"labels":["__name__","BucketName","StorageType"]}` ||
		!strings.HasPrefix(texts[1], "No labels in the last 60 minutes; found 3 over the last 4 days") {
		t.Fatalf("texts = %q, want wider labels plus note", texts)
	}
}

func TestPromqlLabels_ScopedEmptyBothWindowsSaysSo(t *testing.T) {
	cfg, client, got := labelLookupServer(t, `{"labels":null}`)

	result, _, err := NewPromqlLabelsHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelsArgs{
		MatchQuery: "daily_metric",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if len(*got) != 2 {
		t.Fatalf("requests = %d, want 2", len(*got))
	}
	texts := resultTexts(t, result)
	want := "No labels in the last 60 minutes or the last 4 days."
	if len(texts) != 2 || texts[0] != `{"labels":null}` || texts[1] != want {
		t.Fatalf("texts = %q, want [`{\"labels\":null}` %q]", texts, want)
	}
}

func TestPromqlLabelValues_WiderNonArrayResultIsShownWithoutCount(t *testing.T) {
	cfg, client, _ := labelLookupServer(t, `[]`, `{"values":["a"]}`)

	result, _, err := NewPromqlLabelValuesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery: "daily_metric",
		Label:      "env",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	texts := resultTexts(t, result)
	want := "No label values in the last 60 minutes; found a non-empty result over the last 4 days (metric reports sparsely). Showing the wider window."
	if len(texts) != 2 || texts[0] != `{"values":["a"]}` || texts[1] != want {
		t.Fatalf("texts = %q, want the wider body and %q", texts, want)
	}
}

func TestPromqlLabelValues_ScopedEmptyBothWindowsSaysSo(t *testing.T) {
	cfg, client, got := labelLookupServer(t, `[]`)

	result, _, err := NewPromqlLabelValuesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery: "daily_metric",
		Label:      "env",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if len(*got) != 2 {
		t.Fatalf("requests = %d, want 2", len(*got))
	}
	texts := resultTexts(t, result)
	want := "No label values in the last 60 minutes or the last 4 days."
	if len(texts) != 2 || texts[0] != `[]` || texts[1] != want {
		t.Fatalf("texts = %q, want [`[]` %q]", texts, want)
	}
}

func TestPromqlLabelValues_ExplicitWindowNeverRetries(t *testing.T) {
	cases := map[string]PromqlLabelValuesArgs{
		"lookback_minutes":            {LookbackMinutes: 30},
		"lookback_minutes at default": {LookbackMinutes: 60},
		"start_time_iso":              {StartTimeISO: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)},
		"end_time_iso":                {EndTimeISO: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, client, got := labelLookupServer(t, `[]`)
			args.MatchQuery = "daily_metric"
			args.Label = "env"

			result, _, err := NewPromqlLabelValuesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if len(*got) != 1 {
				t.Fatalf("requests = %d, want 1 (explicit window is never widened)", len(*got))
			}
			if texts := resultTexts(t, result); len(texts) != 1 || texts[0] != `[]` {
				t.Fatalf("texts = %q, want the raw empty array only", texts)
			}
		})
	}
}

func TestPromqlLabels_UnscopedEmptyNeverRetries(t *testing.T) {
	for _, query := range []string{`{job="api"}`, `{__name__=~"aws_.*"}`, `sum(rate(http_requests_total[5m]))`, `aws_s3_bucketsizebytes{} or up{}`} {
		t.Run(query, func(t *testing.T) {
			cfg, client, got := labelLookupServer(t, `{"labels":[]}`)

			result, _, err := NewPromqlLabelsHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelsArgs{MatchQuery: query})
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if len(*got) != 1 {
				t.Fatalf("requests = %d, want 1 (unscoped lookups are not widened)", len(*got))
			}
			if texts := resultTexts(t, result); len(texts) != 1 || texts[0] != `{"labels":[]}` {
				t.Fatalf("texts = %q, want the raw empty result only", texts)
			}
		})
	}
}

func TestPromqlLabelValues_NonEmptyDefaultWindowNeverRetries(t *testing.T) {
	cfg, client, got := labelLookupServer(t, `["prod"]`)

	result, _, err := NewPromqlLabelValuesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery: "up",
		Label:      "env",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("requests = %d, want 1", len(*got))
	}
	if texts := resultTexts(t, result); len(texts) != 1 || texts[0] != `["prod"]` {
		t.Fatalf("texts = %q, want the default window's values only", texts)
	}
}

func TestPromqlLabelValues_FailedRetryKeepsDefaultResultWithNote(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{APIBaseURL: srv.URL}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}

	result, _, err := NewPromqlLabelValuesHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery: "daily_metric",
		Label:      "env",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	texts := resultTexts(t, result)
	if len(texts) != 2 || texts[0] != `[]` || !strings.HasPrefix(texts[1], "No label values in the last 60 minutes; retrying over the last 4 days failed: ") {
		t.Fatalf("texts = %q, want the default window's empty array and a failed-retry note", texts)
	}
}

func TestIsMetricScopedSelector(t *testing.T) {
	cases := map[string]bool{
		"up": true,
		` aws_s3_bucketsizebytes{Namespace="AWS/S3"}`: true,
		"node:cpu:rate5m {}":                          true,
		`{__name__="up"}`:                             true,
		`{job="api", __name__ = 'up'}`:                true,
		`{job="api"}`:                                 false,
		`{__name__=~"up|down"}`:                       false,
		`{__name__!="up"}`:                            false,
		"sum(up)":                                     false,
		"up or down":                                  false,
		`up{} or down{}`:                              false,
		`up{job="a"} / down`:                          false,
		`{__name__="up"} or {__name__="down"}`:        false,
		`sum({__name__="up"})`:                        false,
		"":                                            false,
	}
	for query, want := range cases {
		if got := isMetricScopedSelector(query); got != want {
			t.Errorf("isMetricScopedSelector(%q) = %v, want %v", query, got, want)
		}
	}
}
