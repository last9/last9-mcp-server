package prompts

import _ "embed"

//go:embed descriptions/get_logs.md
var GetLogsInstructions string

//go:embed descriptions/get_traces.md
var GetTracesInstructions string

//go:embed descriptions/get_service_traces.md
var GetServiceTracesInstructions string

//go:embed descriptions/get_metrics.md
var GetMetricsInstructions string

//go:embed descriptions/get_exceptions.md
var GetExceptionsInstructions string
