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
var argumentChecks = map[string]argumentCheck{
	"did_you_mean":     withoutToolContext(suggest.PrepareArguments),
	"get_dashboard":    withoutToolContext(dashboards.PrepareGetDashboardArguments),
	"create_dashboard": withoutToolContext(dashboards.PrepareDashboardArguments),
	"update_dashboard": withoutToolContext(dashboards.PrepareDashboardArguments),
	"get_entity_kpis":  checkEntityKPIArguments,
}

type argumentCheck func(json.RawMessage, func(string) bool) (json.RawMessage, error)

func withoutToolContext(check func(json.RawMessage) (json.RawMessage, error)) argumentCheck {
	return func(raw json.RawMessage, _ func(string) bool) (json.RawMessage, error) {
		return check(raw)
	}
}

func checkEntityKPIArguments(raw json.RawMessage, served func(string) bool) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["entity_name"] != nil {
		if served("get_alert_groups") {
			return nil, errors.New("Unknown argument entity_name. Call get_alert_groups with alert_group_name, select the intended group, then pass its id as entity_id to get_entity_kpis.")
		}
		return nil, errors.New("Unknown argument entity_name. Ask for the alert group ID, then pass it as entity_id to get_entity_kpis.")
	}
	return raw, nil
}

func argumentCheckMiddleware(tool string, check argumentCheck, served func(string) bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil && call.Params.Name == tool {
				args, err := check(call.Params.Arguments, served)
				if err != nil {
					return utils.ToolErrorResult(err.Error()), nil
				}
				call.Params.Arguments = args
			}
			return next(ctx, method, req)
		}
	}
}
