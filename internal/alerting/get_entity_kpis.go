package alerting

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetEntityKPIsArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID from get_alert_groups or get_alert_config"`
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

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()),
			Content: []mcp.Content{
				&mcp.TextContent{Text: formatEntityKPIsResponse(kpis)},
			},
		}, nil, nil
	}
}

func formatEntityKPIsResponse(kpis []entities.KPI) string {
	out := fmt.Sprintf("Found %d KPIs for entity:\n\n", len(kpis))
	for i, kpi := range kpis {
		out += fmt.Sprintf("KPI %d:\n", i+1)
		out += fmt.Sprintf("  ID: %s\n", kpi.ID)
		out += fmt.Sprintf("  Name: %s\n", kpi.Name)
		if kpi.Definition.Query != "" {
			out += fmt.Sprintf("  Query: %s\n", kpi.Definition.Query)
		}
		if kpi.Definition.Unit != "" {
			out += fmt.Sprintf("  Unit: %s\n", kpi.Definition.Unit)
		}
		out += "\n"
	}
	return out
}
