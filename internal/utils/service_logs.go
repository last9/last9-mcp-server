package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

// MakeLogsJSONQueryAPI posts a raw log JSON pipeline to the query_range API with the given time range.
func MakeLogsJSONQueryAPI(ctx context.Context, client *http.Client, cfg models.Config, pipeline any, startMs, endMs int64, limit int, index string) (*http.Response, error) {
	// Basic validation
	if client == nil {
		return nil, errors.New("http client cannot be nil")
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return nil, errors.New("API base URL cannot be empty")
	}
	if strings.TrimSpace(cfg.TokenManager.GetAccessToken(ctx)) == "" {
		return nil, errors.New("access token cannot be empty")
	}

	// Build URL
	logsURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointLogsQueryRange)
	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", fmt.Sprintf("%d", startMs/1000)) // seconds
	queryParams.Add("end", fmt.Sprintf("%d", endMs/1000))     // seconds
	queryParams.Add("region", cfg.Region)
	if limit > 0 {
		queryParams.Add("limit", fmt.Sprintf("%d", limit))
	}
	normalizedIndex, err := NormalizeLogIndex(index)
	if err != nil {
		return nil, err
	}
	if normalizedIndex != "" {
		queryParams.Add("index", normalizedIndex)
	}
	fullURL := fmt.Sprintf("%s?%s", logsURL, queryParams.Encode())

	// Build body
	body := map[string]any{
		"pipeline": pipeline,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pipeline: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Headers
	setServiceLogsHeaders(req, cfg.TokenManager.GetAccessToken(ctx))

	// Execute
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	return resp, nil
}

// setServiceLogsHeaders sets the required HTTP headers
func setServiceLogsHeaders(req *http.Request, accessToken string) {
	bearerToken := constants.BearerPrefix + accessToken
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, bearerToken)
}
