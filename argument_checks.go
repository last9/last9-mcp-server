package main

import (
	"context"
	"encoding/json"
	"errors"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentChecks run before the SDK validates a tool's arguments against its schema,
// which stops at the first violation; each check names every problem it finds.
var argumentChecks = map[string]func(json.RawMessage) (json.RawMessage, error){
	"get_service_environments": checkServiceEnvironmentArguments,
	"get_service_traces":       checkServiceTraceArguments,
	"get_alert_config":         alerting.PrepareAlertConfigArguments,
	"did_you_mean":             suggest.PrepareArguments,
	"get_dashboard":            dashboards.PrepareGetDashboardArguments,
	"create_dashboard":         dashboards.PrepareDashboardArguments,
	"update_dashboard":         dashboards.PrepareDashboardArguments,
}

func checkServiceTraceArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["span_name"] != nil {
		return nil, errors.New(`unexpected additional properties ["span_name"]. Call get_traces with tracejson_query=[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","<service_name>"]},{"$eq":["SpanName","<span_name>"]}]}}]. ` +
			`For trace_id, replace the ServiceName condition with {"$eq":["TraceId","<trace_id>"]}. If env was set, add {"$eq":["resources['deployment.environment']","<env>"]} to $and. ` +
			`Preserve the time window and limit. For omitted values, set lookback_minutes to 60 for service_name or 4320 for trace_id, and limit to 10.`)
	}
	return raw, nil
}

func checkServiceEnvironmentArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["service"] != nil {
		return nil, errors.New("unknown argument \"service\". Use service_name to filter environments by service.")
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
