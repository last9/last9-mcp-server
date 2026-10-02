package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListDatasourcesArgs struct{}

func NewListDatasourcesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListDatasourcesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ ListDatasourcesArgs) (*mcp.CallToolResult, any, error) {
		body, err := doJSONRequest(ctx, client, cfg, cfg.GrafanaAPIBaseURL+"/api/datasources")
		if err != nil {
			return nil, nil, err
		}
		// Decode into a private struct that only carries the safe projection;
		// credential fields (user, password, basicAuthUser, secureJsonFields…)
		// are never forwarded to the model.
		var raw []struct {
			ID         int    `json:"id"`
			UID        string `json:"uid,omitempty"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			TypeName   string `json:"typeName,omitempty"`
			URL        string `json:"url,omitempty"`
			Access     string `json:"access,omitempty"`
			IsDefault  bool   `json:"isDefault"`
			IsReadOnly bool   `json:"isReadOnly,omitempty"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, nil, fmt.Errorf("failed to parse datasources response: %w", err)
		}
		out := make([]Datasource, 0, len(raw))
		for _, ds := range raw {
			out = append(out, Datasource{
				ID:         ds.ID,
				UID:        ds.UID,
				Name:       ds.Name,
				Type:       ds.Type,
				TypeName:   ds.TypeName,
				URL:        ds.URL,
				Access:     ds.Access,
				IsDefault:  ds.IsDefault,
				IsReadOnly: ds.IsReadOnly,
			})
		}
		encoded, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}},
		}, nil, nil
	}
}
