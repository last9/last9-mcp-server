package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

// MakeProfilesJSONQueryAPI posts a profiles JSON pipeline to query_range/json.
// start/end are sent as RFC3339 (same as the dashboard ProfilesApis client).
func MakeProfilesJSONQueryAPI(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	pipeline any,
	start, end time.Time,
	limit int,
	region string,
) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("http client cannot be nil")
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return nil, errors.New("API base URL cannot be empty")
	}
	if cfg.TokenManager == nil || strings.TrimSpace(cfg.TokenManager.GetAccessToken(ctx)) == "" {
		return nil, errors.New("access token cannot be empty")
	}
	if limit <= 0 {
		limit = DefaultFlamegraphRowLimit
	}
	if limit > MaxFlamegraphRowLimit {
		limit = MaxFlamegraphRowLimit
	}
	if strings.TrimSpace(region) == "" {
		region = cfg.Region
	}

	profilesURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointProfilesQueryRange)
	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", start.UTC().Format(time.RFC3339))
	queryParams.Add("end", end.UTC().Format(time.RFC3339))
	queryParams.Add("limit", fmt.Sprintf("%d", limit))
	queryParams.Add("region", region)
	fullURL := fmt.Sprintf("%s?%s", profilesURL, queryParams.Encode())

	body := map[string]any{"pipeline": pipeline}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pipeline: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	return resp, nil
}

// runQueryRange executes a pipeline and unwraps dataframe/array rows.
func runQueryRange(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	pipeline any,
	start, end time.Time,
	limit int,
	region string,
) ([]map[string]any, error) {
	resp, err := MakeProfilesJSONQueryAPI(ctx, client, cfg, pipeline, start, end, limit, region)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read profiles response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusBadRequest:
		return nil, fmt.Errorf("profiles API invalid range/query (400): %s", string(body))
	case http.StatusNotFound:
		// Tempo only registers /profiles/api/v1 when ProfilesEnabled for the tenant.
		return nil, fmt.Errorf("profiling is not enabled for your account. Please contact the Last9 team to enable it")
	case http.StatusRequestTimeout:
		return nil, fmt.Errorf("profiles API timed out (408): %s", string(body))
	default:
		return nil, fmt.Errorf("profiles API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode profiles response: %w", err)
	}
	return unwrapQueryRangeRows(decoded), nil
}
