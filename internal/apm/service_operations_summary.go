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

type ServiceOperationsSummaryResponse struct {
	ServiceName string                    `json:"service_name"`
	Env         string                    `json:"env"`
	Operations  []ServiceOperationSummary `json:"operations"`
}

type ServiceOperationSummary struct {
	Name            string             `json:"name"`
	ServiceName     string             `json:"service_name"`
	Env             string             `json:"env"`
	DBSystem        string             `json:"db_system,omitempty"`
	MessagingSystem string             `json:"messaging_system,omitempty"`
	NetPeerName     string             `json:"net_peer_name,omitempty"`
	RPCSystem       string             `json:"rpc_system,omitempty"`
	Throughput      float64            `json:"throughput"`
	ErrorRate       float64            `json:"error_rate"`
	ResponseTime    map[string]float64 `json:"response_time"`
	ErrorPercent    float64            `json:"error_percent"`
}

func NewServiceOperationsSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
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
		// Prepare the Prometheus query for throughput of endpoint operations
		throughputQuery := fmt.Sprintf(
			`sum by (span_name, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", span_kind='SPAN_KIND_SERVER', env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare instant query request to Prometheus
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, throughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var promResp apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&promResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of endpoint operations
		respTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (quantile, span_name, span_kind) (trace_endpoint_duration{service_name="%s", span_kind='SPAN_KIND_SERVER', env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, respTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var respTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&respTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of endpoint operations
		errorRateQuery := fmt.Sprintf(
			`100 * (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name="%s", span_kind='SPAN_KIND_SERVER', env=~"%s", http_status_code=~'4.*|5.*'}[%s])) / %d) / (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name="%s", span_kind='SPAN_KIND_SERVER', env=~"%s"}[%s])) / %d)`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, errorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var errorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&errorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for throughput of database operations
		dbThroughputQuery := fmt.Sprintf(
			`sum by (span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name="%s", span_kind='SPAN_KIND_CLIENT', db_system!='', env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var dbThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of database operations
		dbRespTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (quantile, span_name, db_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name="%s", span_kind='SPAN_KIND_CLIENT', db_system!='', env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var dbRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of database operations
		dbErrorRateQuery := fmt.Sprintf(
			`
			    100 * 
    			(
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env=~"%s", status_code=~"STATUS_CODE_ERROR"} [%s]) / %d)
					or
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env=~"%s", http_status_code=~"4.*|5.*"} [%s]) / %d)
				)  
				/ 
				(
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env=~"%s"} [%s]) / %d)
				)
			`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var dbErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare query for http operations
		httpThroughputQuery := fmt.Sprintf(
			`sum by(span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name="%s", span_kind='SPAN_KIND_CLIENT', env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var httpThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of http operations
		httpRespTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (quantile, span_name, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name="%s", span_kind='SPAN_KIND_CLIENT', env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var httpRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of http operations
		httpErrorRateQuery := fmt.Sprintf(
			`			100 * 
			(
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env=~"%s", status_code=~"STATUS_CODE_ERROR"} [%s]) / %d)
				or
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env=~"%s", http_status_code=~"4.*|5.*"} [%s]) / %d)
			)
			/
			(
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env=~"%s"} [%s]) / %d)
			)`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var httpErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare query for messaging operations
		messagingThroughputQuery := fmt.Sprintf(
			`sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name="%s", messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env=~"%s"}[%s])) / %d`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var messagingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of messaging operations
		messagingRespTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (quantile, span_name, messaging_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name="%s", messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env=~"%s"}[%s]))`,
			escSvc, escEnv, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var messagingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of messaging operations
		messagingErrorRateQuery := fmt.Sprintf(
			`			100 * 
			(
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env=~"%s", status_code=~"STATUS_CODE_ERROR", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
				or
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env=~"%s", http_status_code=~"4.*|5.*", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
			)
			/
			(
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env=~"%s", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
			)`,
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
			escSvc, escEnv, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var messagingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the response structure
		operationsSummary := make([]ServiceOperationSummary, 0)
		for _, r := range promResp {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range respTimeRaw {
				if rt.Metric["span_name"] == operation.Name {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}

			// Find matching error rate data
			for _, er := range errorRateRaw {
				if er.Metric["span_name"] == operation.Name {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
		}
		// Add database operations
		for _, r := range dbThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				DBSystem:    r.Metric["db_system"],
				NetPeerName: r.Metric["net_peer_name"],
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range dbRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["db_system"] == operation.DBSystem &&
					rt.Metric["net_peer_name"] == operation.NetPeerName {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range dbErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["db_system"] == operation.DBSystem &&
					er.Metric["net_peer_name"] == operation.NetPeerName {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// add http operations
		for _, r := range httpThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				NetPeerName: r.Metric["net_peer_name"],
				RPCSystem:   r.Metric["rpc_system"],
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range httpRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["net_peer_name"] == operation.NetPeerName &&
					rt.Metric["rpc_system"] == operation.RPCSystem {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range httpErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["net_peer_name"] == operation.NetPeerName &&
					er.Metric["rpc_system"] == operation.RPCSystem {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// add messaging operations
		for _, r := range messagingThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:            r.Metric["span_name"],
				ServiceName:     serviceName,
				Env:             env,
				MessagingSystem: r.Metric["messaging_system"],
				NetPeerName:     r.Metric["net_peer_name"],
				RPCSystem:       r.Metric["rpc_system"],
				Throughput:      0, // default to 0, will be updated later
				ErrorRate:       0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range messagingRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["messaging_system"] == operation.MessagingSystem &&
					rt.Metric["net_peer_name"] == operation.NetPeerName &&
					rt.Metric["rpc_system"] == operation.RPCSystem {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range messagingErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["messaging_system"] == operation.MessagingSystem &&
					er.Metric["net_peer_name"] == operation.NetPeerName &&
					er.Metric["rpc_system"] == operation.RPCSystem {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// Prepare the final response structure
		details := ServiceOperationsSummaryResponse{
			ServiceName: serviceName,
			Env:         env,
			Operations:  operationsSummary,
		}
		// Return the response
		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAPMServiceLink(startTimeParam*1000, endTimeParam*1000, serviceName, deeplink.APMCatalogEnvExact(env), "operations")

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
