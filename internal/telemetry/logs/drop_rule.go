package logs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"

	"github.com/acrmp/mcp"
)

// NewGetDropRulesHandler creates a handler for getting drop rules for logs
func NewGetDropRulesHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// First refresh the access token
		accessToken, err := utils.RefreshAccessToken(client, cfg)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to refresh access token: %w", err)
		}

		// Extract organization slug from token
		orgSlug, err := utils.ExtractOrgSlugFromToken(accessToken)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to extract organization slug: %w", err)
		}

		// Build request URL with query parameters
		u, err := url.Parse(fmt.Sprintf("%s/api/v4/organizations/%s/logs_settings/routing", cfg.ActionURL, orgSlug))
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()

		u.RawQuery = q.Encode()

		// Create request
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		// Use the new access token
		req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+accessToken)

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(jsonData),
					Type: "text",
				},
			},
		}, nil
	}
}

// NewAddDropRuleHandler creates a handler for adding new drop rules for logs
func NewAddDropRuleHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// First refresh the access token
		accessToken, err := utils.RefreshAccessToken(client, cfg)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to refresh access token: %w", err)
		}

		// Extract organization slug from token
		orgSlug, err := utils.ExtractOrgSlugFromToken(accessToken)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to extract organization slug: %w", err)
		}

		// Build request URL
		u, err := url.Parse(fmt.Sprintf("%s/api/v4/organizations/%s/logs_settings/drop_rule", cfg.ActionURL, orgSlug))
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		// Parse the base URL to get the hostname
		baseURL, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
		}

		// Add region query parameter
		q := u.Query()
		region := utils.GetDefaultRegion(baseURL.Host)
		q.Set("region", region)
		u.RawQuery = q.Encode()

		// Extract individual parameters and build the drop rule
		name, ok := params.Arguments["name"].(string)
		if !ok || name == "" {
			return mcp.CallToolResult{}, errors.New("rule name is required")
		}

		// Extract and validate filters
		filtersRaw, ok := params.Arguments["filters"].([]interface{})
		if !ok {
			return mcp.CallToolResult{}, errors.New("filters are required")
		}

		var filters []models.DropRuleFilter
		for _, f := range filtersRaw {
			filterMap, ok := f.(map[string]string)
			if !ok {
				return mcp.CallToolResult{}, errors.New("invalid filter format")
			}

			// Validate operator
			operator := filterMap["operator"]
			if operator != "equals" && operator != "not_equals" {
				return mcp.CallToolResult{}, fmt.Errorf("invalid operator: %s. Must be one of: [equals, not_equals]", operator)
			}

			// Validate conjunction
			conjunction := filterMap["conjunction"]
			if conjunction != "and" && conjunction != "or" {
				return mcp.CallToolResult{}, fmt.Errorf("invalid conjunction: %s. Must be one of: [and, or]", conjunction)
			}

			filter := models.DropRuleFilter{
				Key:         filterMap["key"],
				Value:       filterMap["value"],
				Operator:    operator,
				Conjunction: conjunction,
			}
			filters = append(filters, filter)
		}

		// Create the complete drop rule
		dropRule := models.DropRule{
			Name:      name,
			Telemetry: TELEMETRY_LOGS,
			Filters:   filters,
			Action: models.DropRuleAction{
				Name:       DROP_RULE_ACTION_NAME,
				Properties: make(map[string]interface{}),
			},
		}

		// Marshal the drop rule
		jsonData, err := json.Marshal(dropRule)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal drop rule: %w", err)
		}

		// Create request
		req, err := http.NewRequest("POST", u.String(), bytes.NewReader(jsonData))
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		// Use the new access token
		req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		responseData, err := json.Marshal(result)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(responseData),
					Type: "text",
				},
			},
		}, nil
	}
}
