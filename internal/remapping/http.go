package remapping

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apiremapping "last9-mcp/internal/last9api/remapping"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ruleTypeLogsExtract = apiremapping.RuleTypeLogsExtract
	ruleTypeLogsMap     = apiremapping.RuleTypeLogsMap
	ruleTypeTracesMap   = apiremapping.RuleTypeTracesMap
)

func validateRuleType(ruleType string) error {
	switch ruleType {
	case ruleTypeLogsExtract, ruleTypeLogsMap, ruleTypeTracesMap:
		return nil
	default:
		return fmt.Errorf("invalid rule_type %q: must be one of [%s, %s, %s]", ruleType, ruleTypeLogsExtract, ruleTypeLogsMap, ruleTypeTracesMap)
	}
}

// newRegionClient builds an API client whose region query is the region argument when
// given, else the configured datasource region.
func newRegionClient(client *http.Client, cfg models.Config, region string) (*last9api.Client, error) {
	if region != "" {
		cfg.Region = region
	}
	if cfg.Region == "" {
		return nil, errors.New("region is required: pass region or configure LAST9_DATASOURCE with a region")
	}
	return last9api.NewClient(client, cfg), nil
}

func remappingResult(cfg models.Config, raw json.RawMessage) (*mcp.CallToolResult, error) {
	dashboardURL := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildRemappingLink()

	// Marshal compacts the body, and renders an empty one as null.
	formatted, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Meta: deeplink.ToMeta(dashboardURL),
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(formatted)},
		},
	}, nil
}
