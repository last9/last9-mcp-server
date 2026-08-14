package profiles

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
)

// Compares path variants. Opt-in via LAST9_LIVE_PROFILES=1.
func TestLiveProfilesPathProbe(t *testing.T) {
	if os.Getenv("LAST9_LIVE_PROFILES") != "1" {
		t.Skip("opt-in")
	}
	cfg := liveProfilesConfig(t)
	token := cfg.TokenManager.GetAccessToken(context.Background())
	client := auth.GetHTTPClient()
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	body := []byte(`{"pipeline":[{"type":"filter","query":{"$eq":["ProfileType","cpu"]}},{"type":"aggregate","function":{"$count":["Value"]},"as":"samples","groupby":{"ServiceName":"name"}},{"type":"select","labels":null,"order":null,"limit":1}]}`)

	type cand struct{ name, base string }
	cands := []cand{
		{"org_profiles", cfg.APIBaseURL + "/profiles/api/v1/query_range/json"},
		{"org_logs_control", cfg.APIBaseURL + "/logs/api/v2/query_range/json"},
	}
	for _, c := range cands {
		q := url.Values{}
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		q.Set("limit", "1")
		q.Set("direction", "backward")
		q.Set("region", cfg.Region)
		req, err := http.NewRequest("POST", c.base+"?"+q.Encode(), bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("%s ERR %v", c.name, err)
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 160))
		resp.Body.Close()
		t.Logf("%s host=%s status=%d body=%q", c.name, hostOf(cfg.APIBaseURL), resp.StatusCode, strings.ReplaceAll(string(b), "\n", " "))
	}
}

func hostOf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		return u[:i]
	}
	return u
}
