package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetEntityKPIsArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID, from get_alert_groups or get_alert_config"`
}

type entityKPIResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Query       string `json:"query"`
	Unit        string `json:"unit"`
}

type entityKPIsResponse struct {
	EntityID string            `json:"entity_id"`
	Count    int               `json:"count"`
	KPIs     []entityKPIResult `json:"kpis"`
}

func NewGetEntityKPIsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetEntityKPIsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetEntityKPIsArgs) (*mcp.CallToolResult, any, error) {
		entityID := strings.TrimSpace(args.EntityID)
		if entityID == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		kpis, err := entities.ListEntityKPIs(ctx, last9api.NewClient(client, cfg), entityID)
		if err != nil {
			return nil, nil, withUpstreamBody(err)
		}
		results := make([]entityKPIResult, 0, len(kpis))
		for _, kpi := range kpis {
			results = append(results, entityKPIResult{
				ID: kpi.ID, Name: kpi.Name, Description: kpi.Description,
				Query: kpi.Definition.Query, Unit: kpi.Definition.Unit,
			})
		}
		sort.Slice(results, func(i, j int) bool {
			if results[i].Name != results[j].Name {
				return results[i].Name < results[j].Name
			}
			return results[i].ID < results[j].ID
		})
		body, err := json.Marshal(entityKPIsResponse{EntityID: entityID, Count: len(results), KPIs: results})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode KPIs: %w", err)
		}
		text := string(body)
		if len(results) == 0 {
			text += "\n\n" + emptyKPIsHint(cfg.ToolFilter)
		}
		link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()
		return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
}

// emptyKPIsHint names create_kpi only when filter serves it: the investigate
// toolset and --read-only serve this tool without it.
func emptyKPIsHint(filter toolsets.Filter) string {
	if filter.ServesWrite("create_kpi") {
		return "This alert group has no KPIs. Create one with create_kpi before writing an alert rule."
	}
	return "This alert group has no KPIs. Creating one needs a session that serves the KPI-creation tool (alerts toolset, not read-only)."
}
