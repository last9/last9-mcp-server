package remapping

import (
	"context"
	"errors"
	"net/http"

	apiremapping "last9-mcp/internal/last9api/remapping"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetRemappingRulesArgs lists remapping rules for a given rule type.
type GetRemappingRulesArgs struct {
	RuleType string `json:"rule_type" jsonschema:"(Required) Remapping rule type: logs_extract, logs_map, or traces_map"`
	Region   string `json:"region,omitempty" jsonschema:"AWS region (defaults to configured datasource region)"`
}

// NewGetRemappingRulesHandler lists remapping rules for the requested type.
func NewGetRemappingRulesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetRemappingRulesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetRemappingRulesArgs) (*mcp.CallToolResult, any, error) {
		if args.RuleType == "" {
			return nil, nil, errors.New("rule_type is required")
		}
		if err := validateRuleType(args.RuleType); err != nil {
			return nil, nil, err
		}

		api, err := newRegionClient(client, cfg, args.Region)
		if err != nil {
			return nil, nil, err
		}

		raw, err := apiremapping.ListRemappingRules(ctx, api, args.RuleType)
		if err != nil {
			return nil, nil, err
		}

		result, err := remappingResult(cfg, raw)
		if err != nil {
			return nil, nil, err
		}
		return result, nil, nil
	}
}
