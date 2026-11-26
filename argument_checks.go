package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentChecks run before the SDK validates a tool's arguments against its schema,
// which stops at the first violation; each check names every problem it finds.
var argumentChecks = map[string]argumentCheck{
	"get_service_environments": withoutToolContext(checkServiceEnvironmentArguments),
	"get_service_traces":       withoutToolContext(checkServiceTraceArguments),
	"get_alert_config":         withoutToolContext(alerting.PrepareAlertConfigArguments),
	"did_you_mean":             withoutToolContext(suggest.PrepareArguments),
	"get_dashboard":            withoutToolContext(dashboards.PrepareGetDashboardArguments),
	"create_dashboard":         withoutToolContext(dashboards.PrepareDashboardArguments),
	"update_dashboard":         withoutToolContext(dashboards.PrepareDashboardArguments),
	"get_entity_kpis":          checkEntityNameArguments("get_entity_kpis"),
	"get_entity_alert_rules":   checkEntityNameArguments("get_entity_alert_rules"),
	"prometheus_label_values":  withoutToolContext(checkLabelValueArguments),
	"describe_alert_chart":     checkDescribeAlertChartArguments,
}

func checkLabelValueArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw, nil
	}
	unknown := make([]string, 0, len(fields))
	argsType := reflect.TypeFor[apm.PromqlLabelValuesArgs]()
	for name := range fields {
		known := false
		for i := range argsType.NumField() {
			field := argsType.Field(i)
			jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if jsonName == name {
				known = true
				break
			}
		}
		if !known {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	const labelHint = "Use label for the label name."
	const selectorHint = "Use match_query for a single PromQL selector string."
	switch {
	case fields["label_name"] != nil && fields["matches"] != nil:
		return nil, fmt.Errorf("unexpected additional properties %q. %s %s", unknown, labelHint, selectorHint)
	case fields["label_name"] != nil:
		return nil, fmt.Errorf("unexpected additional properties %q. %s", unknown, labelHint)
	case fields["matches"] != nil:
		return nil, fmt.Errorf("unexpected additional properties %q. %s", unknown, selectorHint)
	}
	return raw, nil
}

func checkDescribeAlertChartArguments(raw json.RawMessage, served func(string) bool) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["rule_id"] != nil {
		hint := "Unknown argument rule_id. To inspect a chart, omit rule_id and supply surface, chart_key, and service_name."
		if served("get_alert_config") {
			hint += " To inspect an existing rule, call get_alert_config with the same rule_id."
		}
		return nil, errors.New(hint)
	}
	return raw, nil
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

type argumentCheck func(json.RawMessage, func(string) bool) (json.RawMessage, error)

func withoutToolContext(check func(json.RawMessage) (json.RawMessage, error)) argumentCheck {
	return func(raw json.RawMessage, _ func(string) bool) (json.RawMessage, error) {
		return check(raw)
	}
}

func checkEntityNameArguments(tool string) argumentCheck {
	return func(raw json.RawMessage, served func(string) bool) (json.RawMessage, error) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) == nil && fields["entity_name"] != nil {
			if served("get_alert_groups") {
				return nil, errors.New("Unknown argument entity_name. Call get_alert_groups with alert_group_name, select the intended group, then pass its id as entity_id to " + tool + ".")
			}
			return nil, errors.New("Unknown argument entity_name. Ask for the alert group ID, then pass it as entity_id to " + tool + ".")
		}
		return raw, nil
	}
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
