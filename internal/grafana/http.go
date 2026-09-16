package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

const maxAPIErrorBodyBytes = 4096

// maxAPISuccessBodyBytes caps every Grafana API success body. A large
// get_dashboard with full_json=true is the main driver; past this we return an
// error instead of dumping MBs into the client. 5 MiB comfortably exceeds
// normal dashboard payloads.
var maxAPISuccessBodyBytes int64 = 5 * 1024 * 1024

// searchPageSize is the /api/search page we request. Grafana's default page is
// 1000 rows; asking for the same keeps a page at most one round trip but lets
// us page deterministically.
const searchPageSize = 1000

// maxSearchRows bounds how many dashboards one search/folder-listing returns.
// Past this many rows the caller sees truncated=true instead of a silent
// subset; request narrower filters to see the rest.
const maxSearchRows = 5000

// collectSearchHits walks every page of GET /api/search, keeping the caller's
// filters on each page, until a page returns short or the row cap is reached.
// A result whose Dashboards hold exactly maxSearchRows may be incomplete and is
// flagged truncated rather than silently stopping at a page boundary.
func collectSearchHits(ctx context.Context, client *http.Client, cfg models.Config, base string, params url.Values) (SearchResults, error) {
	var all []SearchHit
	for page := 1; len(all) <= maxSearchRows; page++ {
		q := url.Values{}
		for k, vs := range params {
			q[k] = vs
		}
		q.Set("limit", strconv.Itoa(searchPageSize))
		q.Set("page", strconv.Itoa(page))
		body, err := doJSONRequest(ctx, client, cfg, base+"?"+q.Encode())
		if err != nil {
			return SearchResults{}, err
		}
		var hits []SearchHit
		if err := json.Unmarshal(body, &hits); err != nil {
			return SearchResults{}, fmt.Errorf("failed to parse search response: %w", err)
		}
		all = append(all, hits...)
		if len(hits) < searchPageSize {
			break
		}
	}
	res := SearchResults{Dashboards: all}
	if len(all) > maxSearchRows {
		res.Dashboards = all[:maxSearchRows]
		res.Truncated = true
	}
	return res, nil
}

func doJSONRequest(ctx context.Context, client *http.Client, cfg models.Config, url string) ([]byte, error) {
	accessToken := cfg.TokenManager.GetAccessToken(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxAPISuccessBodyBytes+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(respBody)) > maxAPISuccessBodyBytes {
		return nil, fmt.Errorf("grafana API response exceeds %d bytes; narrow the query", maxAPISuccessBodyBytes)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		msg := strings.TrimSpace(string(respBody))
		if len(msg) > maxAPIErrorBodyBytes {
			msg = msg[:maxAPIErrorBodyBytes] + "..."
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return respBody, fmt.Errorf("grafana API returned status %d: %s", resp.StatusCode, msg)
	}

	return respBody, nil
}
