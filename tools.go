package main

import (
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/metrics"
	"last9-mcp/internal/telemetry/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ptr is a helper function to get a pointer to a string
func ptr(s string) *string {
	return &s
}

// registerAllTools returns all tool definitions
func registerAllTools(cfg models.Config) ([]mcp.ToolDefinition, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return []mcp.ToolDefinition{
		{
			Metadata: mcp.Tool{
				Name:        "get_exceptions",
				Description: ptr(traces.GetExceptionsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of exceptions to return",
							"default":     20,
							"minimum":     1,
							"maximum":     100,
						},
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now. Use this for relative time ranges instead of explicit timestamps.",
							"default":     60,
							"minimum":     1,
							"maximum":     1440, // 24 hours
							"examples":    []int{60, 30, 15},
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes. Example: use lookback_minutes instead for relative time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"span_name": map[string]any{
							"type":        "string",
							"description": "Name of the span to filter by",
						},
					},
				},
			},
			Execute:   traces.NewGetExceptionsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_service_summary",
				Description: ptr(apm.GetServiceSummaryDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to end_time_iso - 1 hour. ",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"env": map[string]any{
							"type":        "string",
							"description": "Environment to filter by. Empty string if environment is unknown.",
							"examples":    []string{"production", "prod", "", "staging", "development"},
						},
					},
				},
			},
			Execute:   apm.NewServiceSummaryHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_service_environments",
				Description: ptr(apm.GetServiceEnvironmentsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to end_time_iso - 1 hour. ",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
					},
				},
			},
			Execute:   apm.NewServiceEnvironmentsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		// Add entry for GetServicePerformanceDetails tool
		{
			Metadata: mcp.Tool{
				Name:        "get_service_performance_details",
				Description: ptr(apm.GetServicePerformanceDetails),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"service_name": map[string]any{
							"type":        "string",
							"description": "Name of the service to get performance details for",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"env": map[string]any{
							"type":        "string",
							"description": "Environment to filter by. Empty string if environment is unknown.",
							"examples":    []string{"production", "prod", "", "staging", "development"},
						},
					},
				},
			},
			Execute:   apm.NewServicePerformanceDetailsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		// Add entry for GetServiceOperationsSummary tool
		{
			Metadata: mcp.Tool{
				Name:        "get_service_operations_summary",
				Description: ptr(apm.GetServiceOperationsSummaryDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"service_name": map[string]any{
							"type":        "string",
							"description": "Name of the service to get operations summary for",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"env": map[string]any{
							"type":        "string",
							"description": "Environment to filter by. Empty string if environment is unknown.",
							"examples":    []string{"production", "prod", "", "staging", "development"},
						},
					},
				},
			},
			Execute:   apm.NewServiceOperationsSummaryHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		// Add entry for GetServiceGraph tool
		{
			Metadata: mcp.Tool{
				Name:        "get_service_dependency_graph",
				Description: ptr(apm.GetServiceDependencyGraphDetails),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"env": map[string]any{
							"type":        "string",
							"description": "Environment to filter by. Empty string if environment is unknown.",
							"examples":    []string{"production", "prod", "", "staging", "development"},
						},
						"service_name": map[string]any{
							"type":        "string",
							"description": "Name of the service to get the dependency graph for",
						},
					},
				},
			},
			Execute:   apm.NewServiceDependencyGraphHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		// {
		// 	Metadata: mcp.Tool{
		// 		Name:        "get_service_graph",
		// 		Description: ptr(traces.GetServiceGraphDescription),
		// 		InputSchema: mcp.ToolInputSchema{
		// 			Type: "object",
		// 			Properties: mcp.ToolInputSchemaProperties{
		// 				"span_name": map[string]any{
		// 					"type":        "string",
		// 					"description": "Name of the span to get dependencies for",
		// 				},
		// 				"lookback_minutes": map[string]any{
		// 					"type":        "integer",
		// 					"description": "Number of minutes to look back from now. Use this for relative time ranges instead of explicit timestamps.",
		// 					"default":     60,
		// 					"minimum":     1,
		// 					"maximum":     1440, // 24 hours
		// 					"examples":    []int{60, 30, 15},
		// 				},
		// 				"start_time_iso": map[string]any{
		// 					"type":        "string",
		// 					"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). If specified, lookback_minutes is ignored. Defaults to now - lookback_minutes if not specified.",
		// 					"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
		// 					"examples":    []string{""}, // Empty string to encourage using defaults
		// 				},
		// 			},
		// 		},
		// 	},
		// 	Execute:   traces.NewGetServiceGraphHandler(client, cfg),
		// 	RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		// },
		{
			Metadata: mcp.Tool{
				Name:        "prometheus_range_query",
				Description: ptr(apm.PromqlRangeQueryDetails),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"query": map[string]any{
							"type":        "string",
							"description": "The range query to execute",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
					},
				},
			},
			Execute:   apm.NewPromqlRangeQueryHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		// Add entry for prometheus instance query tool
		{
			Metadata: mcp.Tool{
				Name:        "prometheus_instant_query",
				Description: ptr(apm.PromqlInstantQueryDetails),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"query": map[string]any{
							"type":        "string",
							"description": "The instant query to execute",
						},
						"time_iso": map[string]any{
							"type":        "string",
							"description": "Time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
					},
				},
			},
			Execute:   apm.NewPromqlInstantQueryHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "prometheus_label_values",
				Description: ptr(apm.PromqlLabelValuesQueryDetails),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"match_query": map[string]any{
							"type":        "string",
							"description": "The query to match against",
						},
						"label": map[string]any{
							"type":        "string",
							"description": "The label to get values for",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
					},
				},
			},
			Execute:   apm.NewPromqlLabelValuesHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "prometheus_labels",
				Description: ptr(apm.PromqlLabelsQueryDetails),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"match_query": map[string]any{
							"type":        "string",
							"description": "The query to match against",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
						},
					},
				},
			},
			Execute:   apm.NewPromqlLabelsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_logs",
				Description: ptr(logs.GetLogsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes. Example: use lookback_minutes instead for relative time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now. Use this for relative time ranges instead of explicit timestamps.",
							"default":     60,
							"minimum":     1,
							"maximum":     1440, // 24 hours
							"examples":    []int{60, 30, 15},
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of logs to return",
							"default":     20,
							"minimum":     1,
							"maximum":     100,
						},
						"logjson_query": map[string]any{
							"type":        "array",
							"description": "Optional JSON pipeline query for advanced log filtering and processing.",
							"items": map[string]any{
								"type": "object",
							},
						},
					},
				},
			},
			Execute:   logs.NewGetLogsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_drop_rules",
				Description: ptr(logs.GetDropRulesDescription),
				InputSchema: mcp.ToolInputSchema{
					Type:       "object",
					Properties: mcp.ToolInputSchemaProperties{},
				},
			},
			Execute:   logs.NewGetDropRulesHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "add_drop_rule",
				Description: ptr(logs.AddDropRuleDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"name": map[string]any{
							"type":        "string",
							"description": "Name of the drop rule",
						},
						"filters": map[string]any{
							"type": "array",
							"description": `List of filter conditions.
								e.g.
									"filters": [
										{
											"key": "attributes[\"logtag\"]",
											"value": "P",
											"operator": "equals",
											"conjunction": "and"
										},
										{
											"key": "attributes[\"logtag\"]",
											"value": "F",
											"operator": "not_equals",
											"conjunction": "and"
										}
									]
								regex pattern and filtering on body or message is not supported as of now, will be added in a future version`,
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"key": map[string]any{
										"type": "string",
										"description": `The key to filter on. Must be properly escaped with double quotes.
														For resource attributes, use format: "resource.attributes[\"key_name\"]"
														Example: "resource.attributes[\"service.name\"]"
														For log attributes, use format: "attributes[\"key_name\"]"
														Example: "attributes[\"logtag\"]"`,
									},
									"value": map[string]any{
										"type":        "string",
										"description": "The value to filter against",
									},
									"operator": map[string]any{
										"type":        "string",
										"description": "The comparison operator for the filter condition. Must be one of: [equals, not_equals]. The request will be rejected if any other operator is specified.",
										"enum":        []string{"equals", "not_equals"},
									},
									"conjunction": map[string]any{
										"type":        "string",
										"description": "The logical operator used to combine multiple filter conditions. Must be one of: [and]. The request will be rejected if any other conjunction is specified.",
										"enum":        []string{"and"},
									},
								},
								"required": []string{"key", "value", "operator", "conjunction"},
							},
						},
					},
				},
			},
			Execute:   logs.NewAddDropRuleHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_alert_config",
				Description: ptr(alerting.GetAlertConfigDescription),
				InputSchema: mcp.ToolInputSchema{
					Type:       "object",
					Properties: mcp.ToolInputSchemaProperties{},
				},
			},
			Execute:   alerting.NewGetAlertConfigHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_alerts",
				Description: ptr(alerting.GetAlertsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"timestamp": map[string]any{
							"type":        "integer",
							"description": "Unix timestamp for the query time. Leave empty to default to current time.",
							"examples":    []int{1756228380},
						},
						"window": map[string]any{
							"type":        "integer",
							"description": "Time window in seconds to look back for alerts. Defaults to 900 seconds (15 minutes).",
							"default":     900,
							"minimum":     60,
							"maximum":     86400, // 24 hours
							"examples":    []int{900, 1800, 3600},
						},
					},
				},
			},
			Execute:   alerting.NewGetAlertsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_service_logs",
				Description: ptr(logs.GetServiceLogsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"service_name": map[string]any{
							"type":        "string",
							"description": "Name of the service to get logs for. Only pass the service name. Don't include service keyword",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now. Use this for relative time ranges instead of explicit timestamps.",
							"default":     60,
							"minimum":     1,
							"maximum":     1440,
							"examples":    []int{60, 30, 15},
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of log entries to return",
							"default":     20,
							"minimum":     1,
							"maximum":     100,
						},
						"severity_filters": map[string]any{
							"type":        "array",
							"description": "Optional array of severity level regex patterns to filter logs (e.g., ['ERROR', 'WARN', 'fatal', 'INFO', 'DEBUG', 'info', 'error', 'warn', 'fatal', 'debug'])",
							"items": map[string]any{
								"type": "string",
							},
							"examples": [][]string{
								{"ERROR", "WARN"},
								{"fatal", "error"},
								{"INFO", "DEBUG"},
							},
						},
						"body_filters": map[string]any{
							"type":        "array",
							"description": "Optional array of body regex patterns to filter logs",
							"items": map[string]any{
								"type": "string",
							},
						},
						"env": map[string]any{
							"type":        "string",
							"description": "Environment to filter by. Empty string if environment is unknown.",
							"examples":    []string{"production", "prod", "", "staging", "development"},
						},
					},
				},
			},
			Execute:   logs.NewGetServiceLogsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_service_traces",
				Description: ptr(traces.GetServiceTracesDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"service_name": map[string]any{
							"type":        "string",
							"description": "Name of the service to get traces for (required). Only include the service name. Dont include `service` keyword.",
						},
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now. Use this for relative time ranges instead of explicit timestamps.",
							"default":     60,
							"minimum":     1,
							"maximum":     1440, // 24 hours
							"examples":    []int{60, 30, 15},
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""}, // Empty string to encourage using defaults
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of traces to return",
							"default":     10,
							"minimum":     1,
							"maximum":     100,
						},
						"order": map[string]any{
							"type":        "string",
							"description": "Field to order traces by",
							"default":     "Duration",
							"examples":    []string{"Duration", "Timestamp"},
						},
						"direction": map[string]any{
							"type":        "string",
							"description": "Sort direction for traces",
							"default":     "backward",
							"enum":        []string{"forward", "backward"},
						},
						"span_kind": map[string]any{
							"type":        "array",
							"description": "Filter by span kinds. Accepts user-friendly terms (server, client, internal, consumer, producer) or full constants (SPAN_KIND_SERVER, etc.)",
							"items": map[string]any{
								"type": "string",
								"enum": []string{
									"server", "client", "internal", "consumer", "producer",
									"SPAN_KIND_SERVER", "SPAN_KIND_CLIENT", "SPAN_KIND_INTERNAL", "SPAN_KIND_CONSUMER", "SPAN_KIND_PRODUCER",
								},
							},
						},
						"span_name": map[string]any{
							"type":        "string",
							"description": "Filter by specific span name",
						},
						"status_code": map[string]any{
							"type":        "array",
							"description": "Filter by status codes. Accepts user-friendly terms (ok, error, unset, success) or full constants (STATUS_CODE_OK, etc.)",
							"items": map[string]any{
								"type": "string",
								"enum": []string{
									"ok", "error", "unset", "success",
									"STATUS_CODE_OK", "STATUS_CODE_ERROR", "STATUS_CODE_UNSET",
								},
							},
						},
						"env": map[string]any{
							"type":        "string",
							"description": "Environment to filter by. Empty string if environment is unknown.",
							"examples":    []string{"production", "prod", "", "staging", "development"},
						},
					},
				},
			},
			Execute:   traces.GetServiceTracesHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_log_attributes",
				Description: ptr(logs.GetLogAttributesDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now for the time window",
							"default":     15,
							"minimum":     1,
							"maximum":     1440, // 24 hours
							"examples":    []int{15, 30, 60},
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to use lookback_minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"region": map[string]any{
							"type":        "string",
							"description": "AWS region to query. Leave empty to use default from configuration.",
							"examples":    []string{"ap-south-1", "us-east-1", "eu-west-1"},
						},
					},
				},
			},
			Execute:   logs.NewGetLogAttributesHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_trace_attributes",
				Description: ptr(traces.GetTraceAttributesDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now for the time window",
							"default":     15,
							"minimum":     1,
							"maximum":     1440, // 24 hours
							"examples":    []int{15, 30, 60},
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to use lookback_minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"region": map[string]any{
							"type":        "string",
							"description": "AWS region to query. Leave empty to use default from configuration.",
							"examples":    []string{"ap-south-1", "us-east-1", "eu-west-1"},
						},
					},
				},
			},
			Execute:   traces.NewGetTraceAttributesHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_change_events",
				Description: ptr(change_events.GetChangeEventsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.",
							"pattern":     "^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$",
							"examples":    []string{""},
						},
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back from now. Use this for relative time ranges instead of explicit timestamps.",
							"default":     60,
							"minimum":     1,
							"maximum":     1440, // 24 hours
							"examples":    []int{60, 30, 15},
						},
						"service": map[string]any{
							"type":        "string",
							"description": "Name of the service to filter change events for",
						},
						"environment": map[string]any{
							"type":        "string",
							"description": "Environment to filter by",
						},
						"event_name": map[string]any{
							"type":        "string",
							"description": "Name of the change event to filter by (use available_event_names to see valid values)",
						},
					},
				},
			},
			Execute:   change_events.NewGetChangeEventsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.RequestRateLimit), cfg.RequestRateBurst),
		},
	}, nil
}
