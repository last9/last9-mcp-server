package grafana

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
