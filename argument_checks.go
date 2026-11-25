package main

import (
	"context"
	"encoding/json"
	"errors"

	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentChecks run before the SDK validates a tool's arguments against its schema,
// which stops at the first violation; each check names every problem it finds.
var argumentChecks = map[string]func(json.RawMessage) (json.RawMessage, error){
	"get_trace_attributes_for_pipeline": checkTraceDiscoveryArguments,
	"did_you_mean":                      suggest.PrepareArguments,
	"get_dashboard":                     dashboards.PrepareGetDashboardArguments,
	"create_dashboard":                  dashboards.PrepareDashboardArguments,
	"update_dashboard":                  dashboards.PrepareDashboardArguments,
}

func checkTraceDiscoveryArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["tracejson_query"] != nil {
		return nil, errors.New(`unexpected additional properties ["tracejson_query"]. Use pipeline for the filter stages that scope attribute discovery.`)
	}
	return raw, nil
}

func argumentCheckMiddleware(tool string, check func(json.RawMessage) (json.RawMessage, error)) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil && call.Params.Name == tool {
				args, err := check(call.Params.Arguments)
				if err != nil {
					return utils.ToolErrorResult(err.Error()), nil
				}
				call.Params.Arguments = args
			}
			return next(ctx, method, req)
		}
	}
}
