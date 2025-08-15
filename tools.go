package main

import (
	"last9-mcp/internal/apm"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"
	"net/http"
	"time"

	"github.com/acrmp/mcp"
	"golang.org/x/time/rate"
)

// createTools creates the MCP tool definitions with appropriate rate limits
func createTools(cfg models.Config) ([]mcp.ToolDefinition, error) {
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
							"description": "Environment to filter by. Defaults to 'prod'.",
							"default":     "prod",
						},
					},
				},
			},
			Execute:   apm.NewServiceSummaryHandler(client, cfg),
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
							"description": "Environment to filter by. Defaults to 'prod'.",
							"default":     "prod",
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
							"description": "Environment to filter by. Defaults to 'prod'.",
							"default":     "prod",
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
							"description": "Environment to filter by. Defaults to 'prod'.",
							"default":     "prod",
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
				Name:        "promptheus_range_query",
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
						"service": map[string]any{
							"type":        "string",
							"description": "Name of the service to get logs for",
						},
						"severity": map[string]any{
							"type":        "string",
							"description": "Severity of the logs to get",
						},
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
	}, nil
}

// ptr returns a pointer to the provided string
func ptr(s string) *string {
	return &s
}
