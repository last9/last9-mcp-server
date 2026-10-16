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

type CreateKPIArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that will own the KPI"`
	Name     string `json:"name" jsonschema:"(Required) Human-readable KPI name; used as the indicator name in alert expressions"`
	Query    string `json:"query" jsonschema:"(Required) PromQL expression; verify it returns data with prometheus_range_query first"`
	Unit     string `json:"unit" jsonschema:"(Required) Unit of the query result (e.g. seconds, ratio, bytes); must match thresholds you set later"`
}

func NewCreateKPIHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateKPIArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateKPIArgs) (*mcp.CallToolResult, any, error) {
		entityID := strings.TrimSpace(args.EntityID)
		name := strings.TrimSpace(args.Name)
		query := strings.TrimSpace(args.Query)
		unit := strings.TrimSpace(args.Unit)
		switch {
		case entityID == "":
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		case name == "":
			return utils.ToolErrorResult("name is required"), nil, nil
		case query == "":
			return utils.ToolErrorResult("query is required"), nil, nil
		case unit == "":
			return utils.ToolErrorResult("unit is required"), nil, nil
		}

		kpi, err := entities.CreateEntityKPI(ctx, last9api.NewClient(client, cfg), entities.CreateKPIInput{
			EntityID: entityID,
			Name:     name,
			Query:    query,
			Unit:     unit,
		})
		if err != nil {
			return nil, nil, withUpstreamBody(err)
		}

		text := formatCreatedKPI(kpi)
		return &mcp.CallToolResult{
			Meta:    deeplink.ToMeta(deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()),
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil, nil
	}
}

func formatCreatedKPI(kpi entities.KPI) string {
	out := "Created KPI:\n"
	out += fmt.Sprintf("  ID: %s\n", kpi.ID)
	out += fmt.Sprintf("  Name: %s\n", kpi.Name)
	if kpi.Definition.Query != "" {
		out += fmt.Sprintf("  Query: %s\n", kpi.Definition.Query)
	}
	if kpi.Definition.Unit != "" {
		out += fmt.Sprintf("  Unit: %s\n", kpi.Definition.Unit)
	}
	out += "\nCreate once and keep the returned id; bind it in create_alert expression_args / indicators_used. Do not create a duplicate KPI for the same query."
	return out
}
