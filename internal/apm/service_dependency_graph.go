package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RedMetrics struct {
	Throughput, ResponseTimeP95, ErrorRate, ErrorPercent float64
	ResponseTimeP50, ResponseTimeP90, ResponseTimeAvg    float64
	ResponseTimeMax                                      float64
}

type ServiceDependencyGraphDetails struct {
	ServiceName      string                `json:"service_name"`
	Env              string                `json:"env"`
	Incoming         map[string]RedMetrics `json:"incoming"`
	Outgoing         map[string]RedMetrics `json:"outgoing"`
	MessagingSystems map[string]RedMetrics `json:"messaging_systems"`
	Databases        map[string]RedMetrics `json:"databases"`
}

func NewServiceDependencyGraphHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceDependencyGraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceDependencyGraphArgs) (*mcp.CallToolResult, any, error) {
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		env := args.Env
		if env == "" {
			env = ".*" // default environment
		}
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}
		// Escape once per handler: every PromQL label matcher below must use
		// escSvc/escEnv, never the raw values, to prevent label-matcher injection.
		escSvc, escEnv := utils.EscapePromQLLabel(serviceName), utils.EscapePromQLLabel(env)
		timeRange := fmt.Sprintf("%dm", int((endTimeParam-startTimeParam)/60))

		incoming := make(map[string]RedMetrics)
		outgoing := make(map[string]RedMetrics)
		databases := make(map[string]RedMetrics)
		messagingSystems := make(map[string]RedMetrics)

		// Incoming requests (HTTP server operations):
		// throughput
		incomingThroughputQuery := fmt.Sprintf(
			`sum by (client)(sum_over_time(trace_call_graph_count{server="%s", env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, incomingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var incomingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		incomingRespTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95 ,sum by (client, quantile) (trace_call_graph_duration{server="%s", env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, incomingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var incomingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		incomingErrorRateQuery := fmt.Sprintf(
			`sum by (client)(sum_over_time(trace_call_graph_count{server="%s", env=~"%s", client_status=~'4.*|5.*'}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, incomingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var incomingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process incoming data
		for _, r := range incomingThroughputRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			metrics := RedMetrics{}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			incoming[client] = metrics
		}
		for _, r := range incomingRespTimeRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			quantile := r.Metric["quantile"]
			metrics := incoming[client]
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					case "max":
						metrics.ResponseTimeMax = val
					}
				}
			}
			incoming[client] = metrics
		}
		for _, r := range incomingErrorRateRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			metrics := incoming[client]
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			incoming[client] = metrics
		}
		for client, metrics := range incoming {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			incoming[client] = metrics
		}
		// Outgoing requests (HTTP client operations):
		// throughput
		outgoingThroughputQuery := fmt.Sprintf(
			`sum by (server)(sum_over_time(trace_call_graph_count{client="%s", env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var outgoingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		outgoingRespTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95 ,sum by (server, quantile) (trace_call_graph_duration{client="%s", env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var outgoingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		outgoingErrorRateQuery := fmt.Sprintf(
			`sum by (server)(sum_over_time(trace_call_graph_count{client="%s", env=~"%s", client_status=~'4.*|5.*'}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var outgoingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process outgoing data

		for _, r := range outgoingThroughputRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			metrics := RedMetrics{}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			outgoing[server] = metrics
		}
		for _, r := range outgoingRespTimeRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			quantile := r.Metric["quantile"]
			metrics := outgoing[server]
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					case "max":
						metrics.ResponseTimeMax = val
					}
				}
			}
			outgoing[server] = metrics
		}
		for _, r := range outgoingErrorRateRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			metrics := outgoing[server]
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			outgoing[server] = metrics
		}
		for server, metrics := range outgoing {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			outgoing[server] = metrics
		}
		// Infrastructure services:
		// throughput
		infrastructureThroughputQuery := fmt.Sprintf(
			`sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (sum_over_time(trace_internal_call_graph_count{client="%s", env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var infrastructureThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		infrastructureRespTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95 ,sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service, quantile) (trace_internal_call_graph_duration{client="%s", env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var infrastructureRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		infrastructureErrorRateQuery := fmt.Sprintf(
			`sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (sum_over_time(trace_internal_call_graph_count{client="%s", env=~"%s", client_status=~'4.*|5.*'}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var infrastructureErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process infrastructure data
		for _, r := range infrastructureThroughputRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for _, r := range infrastructureRespTimeRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			quantile := r.Metric["quantile"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
				metrics = databases[key]
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
				metrics = messagingSystems[key]
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					case "max":
						metrics.ResponseTimeMax = val
					}
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for _, r := range infrastructureErrorRateRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
				metrics = databases[key]
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
				metrics = messagingSystems[key]
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for key, metrics := range databases {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			databases[key] = metrics
		}
		for key, metrics := range messagingSystems {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			messagingSystems[key] = metrics
		}
		// Prepare the final response structure
		details := ServiceDependencyGraphDetails{
			ServiceName:      serviceName,
			Env:              env,
			Incoming:         incoming,
			Outgoing:         outgoing,
			Databases:        databases,
			MessagingSystems: messagingSystems,
		}
		// Return the response
		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAPMServiceLink(startTimeParam*1000, endTimeParam*1000, serviceName, deeplink.APMCatalogEnvExact(env), "dependency")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}
