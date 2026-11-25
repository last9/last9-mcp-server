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
	"did_you_mean":        suggest.PrepareArguments,
	"get_dashboard":       dashboards.PrepareGetDashboardArguments,
	"create_dashboard":    dashboards.PrepareDashboardArguments,
	"update_dashboard":    dashboards.PrepareDashboardArguments,
	"get_service_summary": checkServiceSummaryArguments,
}

func checkServiceSummaryArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["service_name"] != nil {
		return nil, errors.New(`unexpected additional properties ["service_name"]. get_service_summary ranks services across the fleet. For one service's performance, call get_service_performance_details with service_name.`)
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
