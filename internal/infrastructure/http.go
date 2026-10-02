package infrastructure

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

const (
	maxAPIErrorBodyBytes   = 4096
	maxAPISuccessBodyBytes = 256 * 1024
)

type jsonCall struct {
	client *http.Client
	cfg    models.Config
	region string
	body   []byte
}

func doJSONRequest(ctx context.Context, call jsonCall) ([]byte, error) {
	if call.cfg.TokenManager == nil {
		return nil, fmt.Errorf("token manager is not configured")
	}

	url := call.cfg.APIBaseURL + constants.EndpointInfrastructureResolve
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(call.body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+call.cfg.TokenManager.GetAccessToken(ctx))
	req.Header.Set(constants.HeaderRegion, call.region)

	resp, err := call.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	return readAPIResponse(resp)
}

func readAPIResponse(resp *http.Response) ([]byte, error) {
	limited := io.LimitReader(resp.Body, maxAPISuccessBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > maxAPISuccessBodyBytes {
		return nil, fmt.Errorf("infrastructure API response exceeds %d bytes", maxAPISuccessBodyBytes)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("infrastructure API returned status %d: %s", resp.StatusCode, truncateAPIError(body, resp.StatusCode))
	}
	return body, nil
}

func truncateAPIError(body []byte, status int) string {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return http.StatusText(status)
	}
	if len(msg) > maxAPIErrorBodyBytes {
		return msg[:maxAPIErrorBodyBytes] + "..."
	}
	return msg
}
