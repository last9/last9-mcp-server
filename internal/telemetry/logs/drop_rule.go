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

// DropRuleFilter represents a filter for drop rules in MCP tool arguments.
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

func dropOTelSettingsListURL(cfg models.Config) (*url.URL, error) {
	if cfg.Region == "" {
		return nil, errors.New("region is not configured")
	}

	u, err := url.Parse(cfg.APIBaseURL + constants.EndpointOTelSettingsDrop)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("region", cfg.Region)
	u.RawQuery = q.Encode()
	return u, nil
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

func validateAddDropRuleArgs(args AddDropRuleArgs) error {
	if args.Name == "" {
		return errors.New("rule name is required")
	}
	if len(args.Filters) == 0 {
		return errors.New("filters must be provided")
	}
	return nil
}

func convertDropRuleFilters(filters []DropRuleFilter) ([]models.DropRuleFilter, error) {
	var converted []models.DropRuleFilter
	for _, f := range filters {
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

		converted = append(converted, models.DropRuleFilter{
			Key:         f.Key,
			Value:       f.Value,
			Operator:    operator,
			Conjunction: conjunction,
		})
	}

	return converted, nil
}

func readDropRuleAPIResponse(resp *http.Response) ([]byte, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("drop rule API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	if !json.Valid(respBody) {
		return nil, fmt.Errorf("drop rule API returned invalid JSON")
	}
	return respBody, nil
}

func dropRuleToolResult(cfg models.Config, responseBody []byte) (*mcp.CallToolResult, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	dashboardURL := dlBuilder.BuildDropRulesLink()

	return &mcp.CallToolResult{
		Meta: deeplink.ToMeta(dashboardURL),
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(responseBody),
			},
		},
	}, nil
}

// NewGetDropRulesHandler creates a handler for getting drop rules for logs.
func NewGetDropRulesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDropRulesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDropRulesArgs) (*mcp.CallToolResult, any, error) {
		accessToken := cfg.TokenManager.GetAccessToken(ctx)

		u, err := dropOTelSettingsListURL(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("get_drop_rules: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("get_drop_rules: failed to create request: %w", err)
		}

		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("get_drop_rules: request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := readDropRuleAPIResponse(resp)
		if err != nil {
			return nil, nil, fmt.Errorf("get_drop_rules: %w", err)
		}

		result, err := dropRuleToolResult(cfg, respBody)
		if err != nil {
			return nil, nil, fmt.Errorf("get_drop_rules: %w", err)
		}
		return result, nil, nil
	}
}

// NewAddDropRuleHandler creates a handler for adding new drop rules for logs
func NewAddDropRuleHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AddDropRuleArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AddDropRuleArgs) (*mcp.CallToolResult, any, error) {
		accessToken := cfg.TokenManager.GetAccessToken(ctx)

		u, err := dropOTelSettingsCreateURL(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: %w", err)
		}

		if err := validateAddDropRuleArgs(args); err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: %w", err)
		}

		filters, err := convertDropRuleFilters(args.Filters)
		if err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: %w", err)
		}

		payload := models.OTelDropSettingCreateRequest{
			Name: args.Name,
			Properties: models.OTelDropSettingProperties{
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
			return nil, nil, fmt.Errorf("add_drop_rule: failed to marshal drop rule: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(jsonData))
		if err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: failed to create request: %w", err)
		}

		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := readDropRuleAPIResponse(resp)
		if err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: %w", err)
		}

		result, err := dropRuleToolResult(cfg, respBody)
		if err != nil {
			return nil, nil, fmt.Errorf("add_drop_rule: %w", err)
		}
		return result, nil, nil
	}
}
