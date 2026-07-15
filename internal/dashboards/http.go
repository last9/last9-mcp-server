package dashboards

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

const maxAPIErrorBodyBytes = 4096

// maxAPISuccessBodyBytes caps dashboard/snapshot success bodies at 5 MiB.
// get_dashboard_snapshot returns the full frozen snapshot (dashboard_definition +
// panel_data), which can be large; past this cap we return an "open the UI link"
// error instead of dumping MBs into the client.
var maxAPISuccessBodyBytes int64 = 5 * 1024 * 1024

func doJSONRequest(ctx context.Context, client *http.Client, cfg models.Config, method, url string, body []byte) ([]byte, int, error) {
	accessToken := cfg.TokenManager.GetAccessToken(ctx)

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	}
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxAPISuccessBodyBytes+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(respBody)) > maxAPISuccessBodyBytes {
		return nil, resp.StatusCode, fmt.Errorf("dashboard API response exceeds %d bytes; open the UI link instead", maxAPISuccessBodyBytes)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		msg := strings.TrimSpace(string(respBody))
		if len(msg) > maxAPIErrorBodyBytes {
			msg = msg[:maxAPIErrorBodyBytes] + "..."
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return respBody, resp.StatusCode, fmt.Errorf("dashboard API returned status %d: %s", resp.StatusCode, msg)
	}

	return respBody, resp.StatusCode, nil
}
