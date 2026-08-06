package pulse

import (
	"context"
	"net/http"
	"net/url"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListRunsArgs struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"Page size from 1 to 100; defaults to 25."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by the prior page."`
}

type GetRunArgs struct {
	RunID string `json:"run_id" jsonschema:"(Required) Pulse run ID."`
}

type GetReportArgs struct {
	RunID string `json:"run_id" jsonschema:"(Required) Pulse run ID."`
}

type RunPageArgs struct {
	RunID  string `json:"run_id" jsonschema:"(Required) Pulse run ID."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Page size from 1 to 100; defaults to 25."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by the prior page."`
}

type GetFindingArgs struct {
	RunID        string `json:"run_id" jsonschema:"(Required) Pulse run ID."`
	OccurrenceID string `json:"occurrence_id" jsonschema:"(Required) Finding occurrence ID returned by list_pulse_findings."`
}

func NewListRunsHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, ListRunsArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListRunsArgs) (*mcp.CallToolResult, any, error) {
		query, err := pageQuery(args.Limit, args.Cursor)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodGet, path: "/runs", query: query})
		return handlerResult(body, err)
	}
}

func NewGetRunHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, GetRunArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetRunArgs) (*mcp.CallToolResult, any, error) {
		return api.getRunResource(ctx, args.RunID, "")
	}
}

func NewGetReportHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, GetReportArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetReportArgs) (*mcp.CallToolResult, any, error) {
		return api.getRunResource(ctx, args.RunID, "/report")
	}
}

func NewListFindingsHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, RunPageArgs) (*mcp.CallToolResult, any, error) {
	return newRunPageHandler(httpClient, config, "/findings")
}

func NewListEvidenceHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, RunPageArgs) (*mcp.CallToolResult, any, error) {
	return newRunPageHandler(httpClient, config, "/evidence")
}

func NewGetFindingHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, GetFindingArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetFindingArgs) (*mcp.CallToolResult, any, error) {
		path, err := findingPath(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodGet, path: path})
		return handlerResult(body, err)
	}
}

func (c *client) getRunResource(ctx context.Context, runID string, suffix string) (*mcp.CallToolResult, any, error) {
	id, err := escapedID(runID)
	if err != nil {
		return nil, nil, err
	}
	body, err := c.call(ctx, request{method: http.MethodGet, path: "/runs/" + id + suffix})
	return handlerResult(body, err)
}

func newRunPageHandler(httpClient *http.Client, config models.Config, suffix string) func(context.Context, *mcp.CallToolRequest, RunPageArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args RunPageArgs) (*mcp.CallToolResult, any, error) {
		path, query, err := runPageRequest(args, suffix)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodGet, path: path, query: query})
		return handlerResult(body, err)
	}
}

func runPageRequest(args RunPageArgs, suffix string) (string, url.Values, error) {
	id, err := escapedID(args.RunID)
	if err != nil {
		return "", nil, err
	}
	query, err := pageQuery(args.Limit, args.Cursor)
	return "/runs/" + id + suffix, query, err
}

func findingPath(args GetFindingArgs) (string, error) {
	runID, err := escapedID(args.RunID)
	if err != nil {
		return "", err
	}
	occurrenceID, err := escapedID(args.OccurrenceID)
	if err != nil {
		return "", err
	}
	return "/runs/" + runID + "/findings/" + occurrenceID, nil
}
