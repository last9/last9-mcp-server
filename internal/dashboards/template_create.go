package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListDashboardTemplatesArgs struct{}

type CreateDashboardFromTemplateArgs struct {
	TemplateID string            `json:"template_id" jsonschema:"Template ID from list_dashboard_templates (e.g. k8s-rightsizing)"`
	Knobs      map[string]string `json:"knobs" jsonschema:"Placeholder values from required_knobs (e.g. DASHBOARD_NAME, NAMESPACES, CLUSTERS, WINDOW)"`
}

func NewListDashboardTemplatesHandler() func(context.Context, *mcp.CallToolRequest, ListDashboardTemplatesArgs) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ ListDashboardTemplatesArgs) (*mcp.CallToolResult, any, error) {
		templates, err := ListTemplates()
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(templates)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
}

func NewCreateDashboardFromTemplateHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateDashboardFromTemplateArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateDashboardFromTemplateArgs) (*mcp.CallToolResult, any, error) {
		if args.TemplateID == "" {
			return nil, nil, errors.New("template_id is required")
		}

		tmpl, err := loadTemplate(args.TemplateID)
		if err != nil {
			return nil, nil, err
		}

		rendered, err := RenderPlaceholders(tmpl, args.Knobs)
		if err != nil {
			return nil, nil, err
		}

		u := cfg.APIBaseURL + constants.EndpointDashboards + "/"
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodPost, u, []byte(rendered))
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return textResultWithDashboardLink(cfg, body, ""), nil, nil
	}
}
