package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListFoldersArgs struct{}

func NewListFoldersHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListFoldersArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ ListFoldersArgs) (*mcp.CallToolResult, any, error) {
		body, err := doJSONRequest(ctx, client, cfg, cfg.GrafanaAPIBaseURL+"/api/folders")
		if err != nil {
			return nil, nil, err
		}
		var folders []Folder
		if err := json.Unmarshal(body, &folders); err != nil {
			return nil, nil, fmt.Errorf("failed to parse folders response: %w", err)
		}
		out, err := json.MarshalIndent(folders, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}
