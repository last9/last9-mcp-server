package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetDropRulesArgs represents the input arguments for getting drop rules (no arguments needed)
type GetDropRulesArgs struct{}

// NewGetDropRulesHandler creates a handler for getting drop rules for logs
func NewGetDropRulesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDropRulesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDropRulesArgs) (*mcp.CallToolResult, any, error) {
		accessToken := cfg.TokenManager.GetAccessToken(ctx)

		// Build request URL with query parameters
		u, err := url.Parse(cfg.ActionURL + fmt.Sprintf(constants.EndpointLogsSettingsRouting, cfg.OrgSlug))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()

		u.RawQuery = q.Encode()

		// Create request
		httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Use the new access token
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)

		// Execute request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL for drop rules (uses default cluster)
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug)
		dashboardURL := dlBuilder.BuildDropRulesLink("default")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(jsonData),
				},
			},
		}, nil, nil
	}
}

// DropRuleFilter represents a filter for drop rules
type DropRuleFilter struct {
	Key         string `json:"key" jsonschema:"The field to filter on (e.g. service level severity)"`
	Value       string `json:"value" jsonschema:"The value to match against (e.g. test-service)"`
	Operator    string `json:"operator" jsonschema:"The operator to use for comparison (default: equals, options: equals, not_equals)"`
	Conjunction string `json:"conjunction" jsonschema:"How to combine with other filters (default: and, options: and)"`
}

// AddDropRuleArgs represents the input arguments for adding drop rules
type AddDropRuleArgs struct {
	Name    string           `json:"name" jsonschema:"Name for the drop rule (e.g. test-service-drop-rule)"`
	Filters []DropRuleFilter `json:"filters" jsonschema:"Array of filter conditions to match logs for dropping"`
}

// NewAddDropRuleHandler creates a handler for adding new drop rules for logs
func NewAddDropRuleHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AddDropRuleArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AddDropRuleArgs) (*mcp.CallToolResult, any, error) {
		// First refresh the access token
		accessToken := cfg.TokenManager.GetAccessToken(ctx)

		// Build request URL
		u, err := url.Parse(cfg.ActionURL + fmt.Sprintf(constants.EndpointLogsSettingsRouting, cfg.OrgSlug))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}

		// Validate required parameters
		if args.Name == "" {
			return nil, nil, errors.New("rule name is required")
		}

		if len(args.Filters) == 0 {
			return nil, nil, errors.New("filters must be provided")
		}

		// Convert filters and validate
		var filters []models.DropRuleFilter
		for _, f := range args.Filters {
			// Set defaults if not provided
			operator := f.Operator
			if operator == "" {
				operator = "equals"
			}
			conjunction := f.Conjunction
			if conjunction == "" {
				conjunction = "and"
			}

			// Validate operator
			if operator != "equals" && operator != "not_equals" {
				return nil, nil, fmt.Errorf("invalid operator: %s. Must be one of: [equals, not_equals]", operator)
			}

			// Validate conjunction
			if conjunction != "and" {
				return nil, nil, fmt.Errorf("invalid conjunction: %s. Must be: [and]", conjunction)
			}

			// Validate key and value
			if f.Key == "" {
				return nil, nil, errors.New("key must be provided")
			}
			if f.Value == "" {
				return nil, nil, errors.New("value must be provided")
			}

			filter := models.DropRuleFilter{
				Key:         f.Key,
				Value:       f.Value,
				Operator:    operator,
				Conjunction: conjunction,
			}
			filters = append(filters, filter)
		}

		// Create the complete drop rule
		dropRule := models.DropRule{
			Name:      args.Name,
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
			return nil, nil, fmt.Errorf("failed to marshal drop rule: %w", err)
		}

		// Create request
		httpReq, err := http.NewRequest("PUT", u.String(), bytes.NewReader(jsonData))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Use the new access token
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)

		// Execute request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}

		responseData, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL for drop rules (uses default cluster)
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug)
		dashboardURL := dlBuilder.BuildDropRulesLink("default")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseData),
				},
			},
		}, nil, nil
	}
}
