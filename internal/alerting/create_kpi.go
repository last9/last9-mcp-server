package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateKPIArgs struct {
	EntityID     string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that will own the KPI"`
	Name         string `json:"name" jsonschema:"(Required) KPI name, unique within the alert group; alert expressions refer to the KPI by an indicator name you choose, not this name"`
	Query        string `json:"query" jsonschema:"(Required) PromQL query; check it returns data with prometheus_range_query first"`
	Unit         string `json:"unit" jsonschema:"(Required) Unit label of the query's raw value, for example seconds, ms, percent, ratio, count, req/s; it does not convert values"`
	Description  string `json:"description,omitempty" jsonschema:"What the KPI measures (optional)"`
	DataSourceID string `json:"data_source_id,omitempty" jsonschema:"Data source UUID to evaluate the query against (optional; defaults to the alert group's data source)"`
}

func NewCreateKPIHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateKPIArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateKPIArgs) (*mcp.CallToolResult, any, error) {
		in := entities.CreateEntityKPIInput{
			EntityID:     strings.TrimSpace(args.EntityID),
			Name:         strings.TrimSpace(args.Name),
			Description:  strings.TrimSpace(args.Description),
			DataSourceID: strings.TrimSpace(args.DataSourceID),
			Query:        strings.TrimSpace(args.Query),
			Unit:         strings.TrimSpace(args.Unit),
		}
		for field, value := range map[string]string{"entity_id": in.EntityID, "name": in.Name, "query": in.Query, "unit": in.Unit} {
			if value == "" {
				return utils.ToolErrorResult(field + " is required"), nil, nil
			}
		}
		kpi, err := entities.CreateEntityKPI(ctx, last9api.NewClient(client, cfg), in)
		var apiErr *utils.UpstreamHTTPError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			return utils.ToolErrorResult(fmt.Sprintf(
				"a KPI named %q already exists on this alert group; call get_entity_kpis and reuse its id instead of creating another", in.Name)), nil, nil
		}
		if err != nil {
			return nil, nil, withUpstreamBody(err)
		}
		if strings.TrimSpace(kpi.ID) == "" {
			return nil, nil, fmt.Errorf("invalid create KPI response: API response is missing id")
		}
		body, err := json.Marshal(entityKPIResult{
			ID: kpi.ID, Name: kpi.Name, Description: kpi.Description, Query: kpi.Definition.Query, Unit: kpi.Definition.Unit,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode KPI: %w", err)
		}
		text := string(body) + "\n\nKeep this id and bind it in expression_args / indicators_used; do not call create_kpi again for the same query."
		return alertMutationResult(cfg, []byte(text), ""), nil, nil
	}
}
