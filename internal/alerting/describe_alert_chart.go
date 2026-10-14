package alerting

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DescribeAlertChartArgs holds the input arguments for describe_alert_chart.
type DescribeAlertChartArgs struct {
	Surface     string            `json:"surface" jsonschema:"(Required) Surface identifier; currently covered surfaces: discover-service, discover-exceptions"`
	ChartKey    string            `json:"chart_key" jsonschema:"(Required) Chart key within the surface, for example apdex, error_rate, response_time, exception_count"`
	ServiceName string            `json:"service_name" jsonschema:"(Required) Service/entity name the chart is scoped to"`
	Env         string            `json:"env,omitempty" jsonschema:"Environment when the chart distinguishes one, for example prod"`
	Attributes  map[string]string `json:"attributes,omitempty" jsonschema:"Extra identity dimensions some charts need; use exactly the key names describe reports"`
}

// NewDescribeAlertChartHandler returns the MCP tool handler for
// describe_alert_chart: a read-only enumerate of the alertable signals on a
// covered chart.
func NewDescribeAlertChartHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DescribeAlertChartArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DescribeAlertChartArgs) (*mcp.CallToolResult, any, error) {
		surface := strings.TrimSpace(args.Surface)
		if surface == "" {
			return utils.ToolErrorResult("surface is required"), nil, nil
		}
		chartKey := strings.TrimSpace(args.ChartKey)
		if chartKey == "" {
			return utils.ToolErrorResult("chart_key is required"), nil, nil
		}
		serviceName := strings.TrimSpace(args.ServiceName)
		if serviceName == "" {
			return utils.ToolErrorResult("service_name is required"), nil, nil
		}

		body, err := alerts.DescribeAlertChart(ctx, last9api.NewClient(client, cfg), alerts.IntelligenceRequest{
			Surface:  surface,
			ChartKey: chartKey,
			Entity:   alertIntelEntity(serviceName, args.Env, args.Attributes),
		})
		if err != nil {
			var apiErr *utils.UpstreamHTTPError
			if errors.As(err, &apiErr) {
				return utils.ToolErrorResult(alertIntelGuidance(apiErr)), nil, nil
			}
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}
