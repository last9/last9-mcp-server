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
	"did_you_mean":            suggest.PrepareArguments,
	"create_dashboard":        dashboards.PrepareDashboardArguments,
	"update_dashboard":        dashboards.PrepareDashboardArguments,
	"prometheus_label_values": checkLabelValueArguments,
}

func checkLabelValueArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw, nil
	}
	const labelHint = "Use label for the label name."
	const selectorHint = "Use match_query for a single PromQL selector string."
	switch {
	case fields["label_name"] != nil && fields["matches"] != nil:
		return nil, errors.New(`unexpected additional properties ["label_name" "matches"]. ` + labelHint + " " + selectorHint)
	case fields["label_name"] != nil:
		return nil, errors.New(`unexpected additional properties ["label_name"]. ` + labelHint)
	case fields["matches"] != nil:
		return nil, errors.New(`unexpected additional properties ["matches"]. ` + selectorHint)
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
