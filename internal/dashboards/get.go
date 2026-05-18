package dashboards

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetDashboardArgs struct {
	ID     string `json:"id" jsonschema:"Dashboard UUID"`
	Region string `json:"region,omitempty" jsonschema:"AWS region for query population (defaults to configured datasource region)"`
}

func resolveRegion(cfg models.Config, arg string) (string, error) {
	if arg != "" {
		return arg, nil
	}
	if cfg.Region != "" {
		return cfg.Region, nil
	}
	return "", errors.New("region is required: pass region or configure LAST9_DATASOURCE with a region")
}

func NewGetDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetDashboardArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == "" {
			return nil, nil, errors.New("id is required")
		}

		region, err := resolveRegion(cfg, args.Region)
		if err != nil {
			return nil, nil, err
		}

		path := fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(args.ID))
		u, err := url.Parse(cfg.APIBaseURL + path)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}
		q := u.Query()
		q.Set("region", region)
		u.RawQuery = q.Encode()

		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, nil, err
		}

		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dlBuilder.BuildDashboardLink(args.ID)),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}
}
