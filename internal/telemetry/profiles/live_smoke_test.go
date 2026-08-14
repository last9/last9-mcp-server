package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Live smoke against a real org. Opt-in only.
//
// Prod (app.last9.io) 404s until last9/last9#11396 (PDE-1117) merges — the
// /profiles/api/v1 Tempo proxy route is only on alpha today. Point at alpha:
//
//	LAST9_LIVE_PROFILES=1 LAST9_API_HOST=alpha.last9.io go test ./internal/telemetry/profiles/ -run TestLiveProfilesSmoke -v
func TestLiveProfilesSmoke(t *testing.T) {
	if os.Getenv("LAST9_LIVE_PROFILES") != "1" {
		t.Skip("set LAST9_LIVE_PROFILES=1 to run")
	}

	cfg := liveProfilesConfig(t)
	t.Logf("org=%s region=%s api_host=%s", cfg.OrgSlug, cfg.Region, hostOf(cfg.APIBaseURL))

	client := auth.GetHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, _, err := NewGetProfileServicesHandler(client, cfg)(ctx, &mcp.CallToolRequest{}, GetProfileServicesArgs{LookbackMinutes: 60})
	if err != nil {
		t.Fatalf("get_profile_services: %v", err)
	}
	if res.IsError {
		text := liveText(res)
		if strings.Contains(text, "last9/last9#11396") || strings.Contains(text, "PDE-1117") {
			t.Skipf("profiles Tempo proxy not on this API host yet: %s", text)
		}
		t.Fatalf("get_profile_services soft-error: %s", text)
	}

	var parsed struct {
		Count    int `json:"count"`
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(liveText(res)), &parsed); err != nil {
		t.Fatalf("decode services: %v", err)
	}
	t.Logf("service_count=%d", parsed.Count)
	if parsed.Count == 0 {
		t.Log("API reachable (200); no profile samples in window")
		return
	}

	svc := parsed.Services[0].Name
	t.Logf("using_service_len=%d", len(svc))

	fg, _, err := NewGetFlamegraphHandler(client, cfg)(ctx, &mcp.CallToolRequest{}, GetFlamegraphArgs{
		Service: svc, LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("get_flamegraph: %v", err)
	}
	if fg.IsError {
		t.Fatalf("get_flamegraph soft-error: %s", liveText(fg))
	}
	var flame struct {
		RowCount     int     `json:"row_count"`
		TotalSamples float64 `json:"total_samples"`
		Truncated    bool    `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(liveText(fg)), &flame); err != nil {
		t.Fatalf("decode flamegraph: %v", err)
	}
	t.Logf("flamegraph row_count=%d total_samples=%v truncated=%v", flame.RowCount, flame.TotalSamples, flame.Truncated)

	top, _, err := NewGetTopFunctionsHandler(client, cfg)(ctx, &mcp.CallToolRequest{}, GetTopFunctionsArgs{
		Service: svc, Limit: 10, LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("get_top_functions: %v", err)
	}
	if top.IsError {
		t.Fatalf("get_top_functions soft-error: %s", liveText(top))
	}
	var tops struct {
		FunctionCount int     `json:"function_count"`
		TotalSamples  float64 `json:"total_samples"`
	}
	if err := json.Unmarshal([]byte(liveText(top)), &tops); err != nil {
		t.Fatalf("decode top: %v", err)
	}
	t.Logf("top_functions function_count=%d total_samples=%v", tops.FunctionCount, tops.TotalSamples)

	sum, _, err := NewGetProfileSummaryHandler(client, cfg)(ctx, &mcp.CallToolRequest{}, GetProfileSummaryArgs{
		Service: svc, TopN: 3, LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("get_profile_summary: %v", err)
	}
	if sum.IsError {
		t.Fatalf("get_profile_summary soft-error: %s", liveText(sum))
	}
	var summary struct {
		Summary      string  `json:"summary"`
		TotalSamples float64 `json:"total_samples"`
	}
	if err := json.Unmarshal([]byte(liveText(sum)), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	t.Logf("summary_len=%d total_samples=%v", len(summary.Summary), summary.TotalSamples)
}

func liveProfilesConfig(t *testing.T) models.Config {
	t.Helper()
	// Prefer shared token exchange (walks up to repo .env), then optionally
	// retarget APIBaseURL at alpha where the profiles Tempo proxy exists.
	base, err := utils.SetupTestConfig()
	if err != nil {
		t.Skipf("skip live smoke: %v", err)
	}
	cfg := *base
	if host := strings.TrimSpace(os.Getenv("LAST9_API_HOST")); host != "" {
		cfg.APIHost = host
		cfg.APIBaseURL = fmt.Sprintf("https://%s/api/v4/organizations/%s", host, cfg.OrgSlug)
	}
	return cfg
}

func liveText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if t, ok := r.Content[0].(*mcp.TextContent); ok {
		return t.Text
	}
	return ""
}
