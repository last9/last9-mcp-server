package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

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
	if json.Unmarshal(raw, &fields) != nil {
		return raw, nil
	}
	if _, exists := fields["tracejson_query"]; !exists {
		return raw, nil
	}

	allowed := map[string]bool{
		"pipeline":         true,
		"lookback_minutes": true,
		"start_time_iso":   true,
		"end_time_iso":     true,
		"region":           true,
	}
	unknown := make([]string, 0, len(fields))
	for field := range fields {
		if !allowed[field] {
			unknown = append(unknown, field)
		}
	}
	sort.Strings(unknown)
	encoded, _ := json.Marshal(unknown)
	return nil, fmt.Errorf("unexpected additional properties %s. Use pipeline for the filter stages that scope attribute discovery.", encoded)
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
