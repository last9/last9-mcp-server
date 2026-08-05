package logs

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

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetDropRulesArgs represents the input arguments for getting drop rules (no arguments needed)
type GetDropRulesArgs struct{}

// NewGetDropRulesHandler creates a handler for getting drop rules for logs.
// Reads still use GET /logs_settings/routing (legacy read path kept by last9-api for backward compat).
func NewGetDropRulesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDropRulesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDropRulesArgs) (*mcp.CallToolResult, any, error) {
		accessToken := cfg.TokenManager.GetAccessToken(ctx)

		u, err := url.Parse(cfg.APIBaseURL + constants.EndpointLogsSettingsRouting)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}

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

		// Build deep link URL for drop rules
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildDropRulesLink()

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
	Key         string `json:"key" jsonschema:"The field to filter on (e.g. attributes[\"level\"], resource.attributes[\"service.name\"])"`
	Value       string `json:"value" jsonschema:"The value to match against (e.g. debug)"`
	Operator    string `json:"operator" jsonschema:"The operator to use for comparison (default: equals, options: equals, not_equals)"`
	Conjunction string `json:"conjunction" jsonschema:"How to combine with other filters (default: and, options: and)"`
}

// AddDropRuleArgs represents the input arguments for adding drop rules
type AddDropRuleArgs struct {
	Name    string           `json:"name" jsonschema:"Name for the drop rule (e.g. test-service-drop-rule)"`
	Filters []DropRuleFilter `json:"filters" jsonschema:"Array of filter conditions to match logs for dropping"`
}

type dropOTelSettingCreateReq struct {
	Name       string                    `json:"name"`
	Properties dropOTelSettingProperties `json:"properties"`
}

type dropOTelSettingProperties struct {
	Telemetry string                  `json:"telemetry"`
	Filters   []models.DropRuleFilter `json:"filters"`
	Action    models.DropRuleAction   `json:"action"`
}

func dropOTelSettingsCreateURL(cfg models.Config) (*url.URL, error) {
	if cfg.Region == "" {
		return nil, errors.New("region is not configured")
	}
	if cfg.ClusterID == "" {
		return nil, errors.New("cluster_id is not configured")
	}

	u, err := url.Parse(cfg.APIBaseURL + constants.EndpointOTelSettingsDrop)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("region", cfg.Region)
	q.Set("cluster_id", cfg.ClusterID)
	u.RawQuery = q.Encode()
	return u, nil
}

func validateAndConvertDropRuleFilters(args AddDropRuleArgs) ([]models.DropRuleFilter, error) {
	if args.Name == "" {
		return nil, errors.New("rule name is required")
	}
	if len(args.Filters) == 0 {
		return nil, errors.New("filters must be provided")
	}

	var filters []models.DropRuleFilter
	for _, f := range args.Filters {
		operator := f.Operator
		if operator == "" {
			operator = "equals"
		}
		conjunction := f.Conjunction
		if conjunction == "" {
			conjunction = "and"
		}

		if operator != "equals" && operator != "not_equals" {
			return nil, fmt.Errorf("invalid operator: %s. Must be one of: [equals, not_equals]", operator)
		}
		if conjunction != "and" {
			return nil, fmt.Errorf("invalid conjunction: %s. Must be: [and]", conjunction)
		}
		if f.Key == "" {
			return nil, errors.New("key must be provided")
		}
		if f.Value == "" {
			return nil, errors.New("value must be provided")
		}
		if !strings.HasPrefix(f.Key, "attributes[") && !strings.HasPrefix(f.Key, "resource.attributes[") {
			return nil, errors.New(`filter key must use attributes["..."] or resource.attributes["..."] format`)
		}

		filters = append(filters, models.DropRuleFilter{
			Key:         f.Key,
			Value:       f.Value,
			Operator:    operator,
			Conjunction: conjunction,
		})
	}

	return filters, nil
}

// NewAddDropRuleHandler creates a handler for adding new drop rules for logs
func NewAddDropRuleHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AddDropRuleArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AddDropRuleArgs) (*mcp.CallToolResult, any, error) {
		accessToken := cfg.TokenManager.GetAccessToken(ctx)

		u, err := dropOTelSettingsCreateURL(cfg)
		if err != nil {
			return nil, nil, err
		}

		filters, err := validateAndConvertDropRuleFilters(args)
		if err != nil {
			return nil, nil, err
		}

		payload := dropOTelSettingCreateReq{
			Name: args.Name,
			Properties: dropOTelSettingProperties{
				Telemetry: TELEMETRY_LOGS,
				Filters:   filters,
				Action: models.DropRuleAction{
					Name:        DROP_RULE_ACTION_NAME,
					Destination: "",
					Properties:  make(map[string]interface{}),
				},
			},
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal drop rule: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(jsonData))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, nil, fmt.Errorf("drop rule API request failed with status %d: %s", resp.StatusCode, string(respBody))
		}

		responseData, err := json.Marshal(json.RawMessage(respBody))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildDropRulesLink()

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
