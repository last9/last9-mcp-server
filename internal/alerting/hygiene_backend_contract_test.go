package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The hygiene API takes an RFC3339 range and returns counts as availability objects
// plus an upstream truncation flag; the tool must request and decode that contract.
func TestHygieneSelectedBackendContract(t *testing.T) {
	const selected = `{"period":{"from":"2026-10-27T00:00:00Z","to":"2026-11-03T00:00:00Z","timezone":"UTC"},
		"coverage":{"lifecycle":{"status":"complete"},"delivery":{"status":"complete"}},
		"rules":[{"rule_id":"synthetic","fire_count":{"status":"available","count":70},"flaps_per_day":{"status":"available","value":5},
		"ack_rate":{"status":"available","value":0}}],"truncated":true}`
	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/alerts/hygiene":
			query = r.URL.Query()
			fmt.Fprint(w, selected)
		case "/alert-rules":
			fmt.Fprint(w, `[{"id":"synthetic","algorithm":"zscore","alert_condition":"count_true(result) >= 3","eval_window":10}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cfg := models.Config{APIBaseURL: srv.URL, OrgSlug: "synthetic", TokenManager: &auth.TokenManager{AccessToken: "synthetic", ExpiresAt: time.Now().Add(time.Hour)}}

	result, _, err := NewGetAlertHygieneHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetAlertHygieneArgs{Window: "30d"})
	if err != nil {
		t.Fatalf("expected a report, got %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected a report, got %+v", result)
	}

	from, errFrom := time.Parse(time.RFC3339, first(query["from"]))
	to, errTo := time.Parse(time.RFC3339, first(query["to"]))
	if errFrom != nil || errTo != nil || to.Sub(from) != 30*24*time.Hour || first(query["timezone"]) != "UTC" {
		t.Errorf("query = %v, want an RFC3339 30-day range in UTC", query)
	}
	if since := time.Since(to); since < 0 || since > 2*time.Minute {
		t.Errorf("to = %s, want now", to)
	}

	var out struct {
		From              string `json:"from"`
		UpstreamTruncated bool   `json:"upstream_truncated"`
		Rules             []struct {
			Findings []hygieneFinding `json:"findings"`
		} `json:"rules"`
	}
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.UpstreamTruncated || out.From != "2026-10-27T00:00:00Z" {
		t.Errorf("upstream_truncated=%v from=%q, want true and the reported period", out.UpstreamTruncated, out.From)
	}
	if len(out.Rules) != 1 || len(out.Rules[0].Findings) != 2 || out.Rules[0].Findings[0].SuggestedFix.Current != "3" {
		t.Errorf("rules = %+v, want noisy and flapping with bad_minutes from the rule configuration", out.Rules)
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
