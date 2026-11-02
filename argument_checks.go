package main

import (
	"context"
	"encoding/json"

	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentChecks run before the SDK validates a tool's arguments against its schema,
// which stops at the first violation; each check names every problem it finds.
var argumentChecks = map[string]func(json.RawMessage) error{
	"create_dashboard": dashboards.CheckDashboardArguments,
	"update_dashboard": dashboards.CheckDashboardArguments,
}

// argumentCheckMiddleware refuses a call to tool whose arguments fail check.
func argumentCheckMiddleware(tool string, check func(json.RawMessage) error) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil && call.Params.Name == tool {
				if err := check(call.Params.Arguments); err != nil {
					return utils.ToolErrorResult(err.Error()), nil
				}
			}
			return next(ctx, method, req)
		}
	}
}
