package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListFolderDashboardsArgs struct {
	FolderUID string `json:"folder_uid" jsonschema:"(Required) Grafana folder UID from grafana_list_folders"`
}

func NewListFolderDashboardsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListFolderDashboardsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListFolderDashboardsArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.FolderUID) == "" {
			return nil, nil, fmt.Errorf("folder_uid is required")
		}

		// The Grafana API proxy 404s /api/folders/{uid}/dashboards, so resolve
		// the folder's numeric id first and list via /api/search?folderIds=.
		folderBody, err := doJSONRequest(ctx, client, cfg, cfg.GrafanaAPIBaseURL+"/api/folders/"+url.PathEscape(args.FolderUID))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch folder %q: %w", args.FolderUID, err)
		}
		var folder Folder
		if err := json.Unmarshal(folderBody, &folder); err != nil {
			return nil, nil, fmt.Errorf("failed to parse folder response: %w", err)
		}
		if folder.ID == 0 {
			return nil, nil, fmt.Errorf("folder %q not found", args.FolderUID)
		}

		u := cfg.GrafanaAPIBaseURL + "/api/search?" + url.Values{
			"folderIds": {strconv.Itoa(folder.ID)},
			"type":      {"dash-db"},
		}.Encode()
		body, err := doJSONRequest(ctx, client, cfg, u)
		if err != nil {
			return nil, nil, err
		}
		var hits []SearchHit
		if err := json.Unmarshal(body, &hits); err != nil {
			return nil, nil, fmt.Errorf("failed to parse folder dashboards response: %w", err)
		}
		out, err := json.MarshalIndent(hits, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}
