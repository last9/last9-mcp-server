package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

// LogSearchMaxSampleSize is the server-side maximum for log search limits.
// The log search endpoint rejects a larger limit. Clamped here so an operator's
// --max_get_logs_entries cannot turn into a failure on every request.
const LogSearchMaxSampleSize = 5000

// LogSearchRequest is one server-side log search, in the terms every log tool
// already has. Deliberately free of GetLogsArgs, deeplinks and tool-specific
// limits so a second caller adopts it by swapping one function call.
type LogSearchRequest struct {
	Pipeline any
	// StartMs and EndMs are milliseconds; the wire format is seconds.
	StartMs int64
	EndMs   int64
	// Limit of 0 is omitted. The endpoint rejects a limit on aggregate and
	// dataframe pipelines, so callers must leave it unset for those.
	Limit int
	// Index is normalized and validated here; a bare name is an error rather
	// than a silent query against the default table.
	Index string
	// Direction empty is omitted, letting the server apply its own default.
	Direction string
}

func (r LogSearchRequest) body(region string) (map[string]any, error) {
	index, err := NormalizeLogIndex(r.Index)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"region":   region,
		"start":    r.StartMs / 1000,
		"end":      r.EndMs / 1000,
		"pipeline": r.Pipeline,
	}
	if index != "" {
		body["index"] = index
	}
	if r.Direction != "" {
		body["direction"] = r.Direction
	}
	if r.Limit > 0 {
		limit := r.Limit
		if limit > LogSearchMaxSampleSize {
			limit = LogSearchMaxSampleSize
		}
		body["limit"] = limit
	}
	return body, nil
}

// MakeLogSearchAPI posts one search to the server-side log search endpoint.
// Non-200 responses are returned rather than raised, so the caller can run them
// through NewUpstreamHTTPError with its own operation name and hint.
func MakeLogSearchAPI(
	ctx context.Context, client *http.Client, cfg models.Config, req LogSearchRequest,
) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("http client cannot be nil")
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return nil, errors.New("API base URL cannot be empty")
	}
	if strings.TrimSpace(cfg.TokenManager.GetAccessToken(ctx)) == "" {
		return nil, errors.New("access token cannot be empty")
	}

	body, err := req.body(cfg.Region)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log search request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, cfg.APIBaseURL+constants.EndpointLogSearch, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	setServiceLogsHeaders(httpReq, cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	return resp, nil
}
