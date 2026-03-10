package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

const (
	logIndexPhysicalPrefix    = "physical_index:"
	logIndexRehydrationPrefix = "rehydration_index:"
)

type logIndexKind string

const (
	logIndexKindPhysical    logIndexKind = "physical"
	logIndexKindRehydration logIndexKind = "rehydration"
)

type parsedLogIndex struct {
	kind  logIndexKind
	name  string
	value string
}

func NormalizeLogIndex(index string) (string, error) {
	if strings.TrimSpace(index) == "" {
		return "", nil
	}

	parsed, err := parseLogIndex(index)
	if err != nil {
		return "", err
	}

	return parsed.value, nil
}

func ResolveLogIndexDashboardParam(ctx context.Context, client *http.Client, cfg models.Config, index string) (string, error) {
	if strings.TrimSpace(index) == "" {
		return "", nil
	}

	parsed, err := parseLogIndex(index)
	if err != nil {
		return "", err
	}

	switch parsed.kind {
	case logIndexKindPhysical:
		id, err := fetchPhysicalIndexID(ctx, client, cfg, parsed.name)
		if err != nil {
			return "", err
		}
		return "physical:" + id, nil
	case logIndexKindRehydration:
		id, err := fetchRehydrationIndexID(ctx, client, cfg, parsed.name)
		if err != nil {
			return "", err
		}
		return "rehydration:" + id, nil
	default:
		return "", fmt.Errorf("unsupported log index kind %q", parsed.kind)
	}
}

func parseLogIndex(index string) (parsedLogIndex, error) {
	trimmed := strings.TrimSpace(index)
	switch {
	case strings.HasPrefix(trimmed, logIndexPhysicalPrefix):
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, logIndexPhysicalPrefix))
		if name == "" {
			return parsedLogIndex{}, fmt.Errorf("index must include a physical index name after %q", logIndexPhysicalPrefix)
		}
		return parsedLogIndex{
			kind:  logIndexKindPhysical,
			name:  name,
			value: logIndexPhysicalPrefix + name,
		}, nil
	case strings.HasPrefix(trimmed, logIndexRehydrationPrefix):
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, logIndexRehydrationPrefix))
		if name == "" {
			return parsedLogIndex{}, fmt.Errorf("index must include a rehydration block name after %q", logIndexRehydrationPrefix)
		}
		return parsedLogIndex{
			kind:  logIndexKindRehydration,
			name:  name,
			value: logIndexRehydrationPrefix + name,
		}, nil
	default:
		return parsedLogIndex{}, fmt.Errorf("index must use physical_index:<name> or rehydration_index:<block_name>")
	}
}

func fetchPhysicalIndexID(ctx context.Context, client *http.Client, cfg models.Config, name string) (string, error) {
	var response struct {
		Properties []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"properties"`
	}

	if err := fetchLogsSettings(ctx, client, cfg, "/logs_settings/physical_indexes", &response); err != nil {
		return "", err
	}

	for _, physicalIndex := range response.Properties {
		if physicalIndex.Name == name {
			if physicalIndex.ID == "" {
				return "", fmt.Errorf("physical index %q was returned without an id", name)
			}
			return physicalIndex.ID, nil
		}
	}

	return "", fmt.Errorf("physical index %q not found", name)
}

func fetchRehydrationIndexID(ctx context.Context, client *http.Client, cfg models.Config, blockName string) (string, error) {
	var response struct {
		Properties []struct {
			ID        string `json:"id"`
			BlockName string `json:"block_name"`
		} `json:"properties"`
	}

	if err := fetchLogsSettings(ctx, client, cfg, "/logs_settings/rehydration", &response); err != nil {
		return "", err
	}

	for _, rehydrationIndex := range response.Properties {
		if rehydrationIndex.BlockName == blockName {
			if rehydrationIndex.ID == "" {
				return "", fmt.Errorf("rehydration index %q was returned without an id", blockName)
			}
			return rehydrationIndex.ID, nil
		}
	}

	return "", fmt.Errorf("rehydration index %q not found", blockName)
}

func fetchLogsSettings(ctx context.Context, client *http.Client, cfg models.Config, endpoint string, target any) error {
	queryParams := url.Values{}
	queryParams.Set("region", cfg.Region)

	apiURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, endpoint, queryParams.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create logs settings request: %w", err)
	}

	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
	req.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute logs settings request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("logs settings request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode logs settings response: %w", err)
	}

	return nil
}
