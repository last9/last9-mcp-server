package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentChecks run before the SDK validates a tool's arguments against its schema,
// which stops at the first violation; each check names every problem it finds.
var argumentChecks = map[string]argumentCheck{
	"get_metric_status":        checkMetricStatusArguments,
	"get_service_environments": withoutToolContext(checkServiceEnvironmentArguments),
	"get_traces":               withoutToolContext(checkGetTracesArguments),
	"get_service_traces":       withoutToolContext(checkServiceTraceArguments),
	"get_alerts":               checkAlertsRangeArguments,
	"get_alert_config":         withoutToolContext(alerting.PrepareAlertConfigArguments),
	"did_you_mean":             withoutToolContext(suggest.PrepareArguments),
	"get_dashboard":            withoutToolContext(dashboards.PrepareGetDashboardArguments),
	"create_dashboard":         withoutToolContext(dashboards.PrepareDashboardArguments),
	"update_dashboard":         withoutToolContext(dashboards.PrepareDashboardArguments),
	"get_entity_kpis":          checkEntityNameArguments("get_entity_kpis"),
	"get_entity_alert_rules":   checkEntityNameArguments("get_entity_alert_rules"),
	"describe_alert_chart":     checkDescribeAlertChartArguments,
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

func checkAlertsRangeArguments(raw json.RawMessage, served func(string) bool) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && (fields["start_time"] != nil || fields["end_time"] != nil) {
		hint := "get_alerts takes a snapshot. For alert instances, set time_iso to end_time in RFC3339 and window to the interval length in seconds (1-3600). Explain the one-hour limit for longer intervals instead of shortening them."
		if served("get_alert_rule_state") {
			hint += " For sampled firing history, call get_alert_rule_state with the original start_time, end_time and any rule_name, plus a step in seconds."
		}
		return nil, errors.New(hint)
	}
	return raw, nil
}

func checkGetTracesArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["response_mode"] != nil {
		return nil, errors.New("response_mode is not supported by get_traces. Do not infer which result was intended: to retrieve matching span rows, omit response_mode and preserve the existing tracejson_query stages and time bounds; to obtain a count or percentile, append an aggregate stage and preserve the existing filters and time bounds.")
	}
	return raw, nil
}

func checkMetricStatusArguments(raw json.RawMessage, served func(string) bool) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields["metric_name_pattern"] != nil {
		hint := `Unknown argument metric_name_pattern. To check status across matching series, put your regex in a PromQL selector in metric (for example {__name__=~"example_.+"}) and supply window_minutes. Use a narrower selector or smaller window if the query is too broad.`
		if served("prometheus_label_values") {
			hint += ` To list matching metric names, call prometheus_label_values with label "__name__" and the selector in match_query.`
		}
		return nil, errors.New(hint)
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

func logPatternArgumentCheck(properties map[string]any) argumentCheck {
	return withoutToolContext(func(raw json.RawMessage) (json.RawMessage, error) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return raw, nil
		}
		hasAlias := false
		for key := range fields {
			hasAlias = hasAlias || strings.EqualFold(key, "service") || strings.EqualFold(key, "body")
		}
		if !hasAlias {
			return raw, nil
		}
		var problems []string
		for key := range fields {
			switch strings.ToLower(key) {
			case "service":
				if fields["service_name"] != nil {
					problems = append(problems, fmt.Sprintf(`Unknown argument %q. service_name is already set; remove %s.`, key, key))
				} else {
					problems = append(problems, fmt.Sprintf(`Unknown argument %q. Use service_name to select the service.`, key))
				}
			case "body":
				if fields["body_filters"] != nil {
					problems = append(problems, fmt.Sprintf(`Unknown argument %q. body_filters is already set; remove %s.`, key, key))
				} else {
					problems = append(problems, fmt.Sprintf(`Unknown argument %q. Use body_filters with an array of message substrings, for example ["timeout"].`, key))
				}
			default:
				if _, known := properties[key]; !known {
					problems = append(problems, fmt.Sprintf("Unknown argument %q.", key))
				}
			}
		}
		sort.Strings(problems)
		return nil, errors.New(strings.Join(problems, " "))
	})
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
