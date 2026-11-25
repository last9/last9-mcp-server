package loops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apiloops "last9-mcp/internal/last9api/loops"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyArgs struct{}

type ListLoopTemplatesArgs struct {
	Capability string `json:"capability,omitempty" jsonschema:"Optional capability filter (substring of slug or exact capability label)"`
}

type ListLoopsArgs struct {
	Query string `json:"query,omitempty" jsonschema:"Optional case-insensitive substring of loop name or id"`
}

type GetLoopArgs struct {
	LoopID string `json:"loop_id" jsonschema:"(Required) Loop id from list_loops or create_loop"`
}

func NewGetLoopsStatusHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, emptyArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
		enabled, err := apiloops.ProbeEnabled(ctx, last9api.NewClient(client, cfg))
		if err != nil && !apiloops.IsNotEnabled(err) {
			return nil, nil, err
		}
		return jsonResult(apiloops.StatusResponse(enabled, err), loopsIndexLink(cfg)), nil, nil
	}
}

func NewListLoopTemplatesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListLoopTemplatesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListLoopTemplatesArgs) (*mcp.CallToolResult, any, error) {
		items, err := apiloops.ListTemplates(ctx, last9api.NewClient(client, cfg), args.Capability)
		if err != nil {
			if apiloops.IsNotEnabled(err) {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(map[string]any{"templates": items, "count": len(items)}, loopsIndexLink(cfg)), nil, nil
	}
}

func NewListLoopsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListLoopsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListLoopsArgs) (*mcp.CallToolResult, any, error) {
		items, err := apiloops.List(ctx, last9api.NewClient(client, cfg), args.Query)
		if err != nil {
			if apiloops.IsNotEnabled(err) {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(map[string]any{"loops": items, "count": len(items)}, loopsIndexLink(cfg)), nil, nil
	}
}

func NewGetLoopHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLoopArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetLoopArgs) (*mcp.CallToolResult, any, error) {
		id := strings.TrimSpace(args.LoopID)
		if id == "" {
			return utils.ToolErrorResult("loop_id is required"), nil, nil
		}
		detail, err := apiloops.Get(ctx, last9api.NewClient(client, cfg), id)
		if err != nil {
			if apiloops.IsNotEnabled(err) {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(detail, loopsDetailLink(cfg, id)), nil, nil
	}
}

func jsonResult(v any, link string) *mcp.CallToolResult {
	body, err := json.Marshal(v)
	if err != nil {
		return utils.ToolErrorResult(fmt.Sprintf("encode response: %v", err))
	}
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}
	if link != "" {
		result.Meta = deeplink.ToMeta(link)
	}
	return result
}

func loopsIndexLink(cfg models.Config) string {
	return deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildLoopsLink()
}

func loopsDetailLink(cfg models.Config, id string) string {
	return deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildLoopLink(id)
}
