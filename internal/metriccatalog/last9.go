package metriccatalog

type Last9Definition struct {
	Type     string
	Unit     string
	Help     string
	Quantile bool
}

var last9Metrics = map[string]Last9Definition{
	"trace_service_response_time":        {"gauge", "milliseconds", "Pre-windowed service response time; select the quantile label directly.", true},
	"trace_endpoint_duration":            {"gauge", "milliseconds", "Pre-windowed endpoint duration; select the quantile label directly.", true},
	"trace_client_duration":              {"gauge", "milliseconds", "Pre-windowed client duration; select the quantile label directly.", true},
	"trace_call_graph_duration":          {"gauge", "milliseconds", "Pre-windowed dependency duration; select the quantile label directly.", true},
	"trace_internal_call_graph_duration": {"gauge", "milliseconds", "Pre-windowed internal dependency duration; select the quantile label directly.", true},
	"trace_endpoint_count":               {"gauge", "count", "Span count per rollup window, not a cumulative counter.", false},
	"trace_internal_count":               {"gauge", "count", "Internal span count per rollup window, not a cumulative counter.", false},
	"trace_client_count":                 {"gauge", "count", "Client span count per rollup window, not a cumulative counter.", false},
	"trace_call_graph_count":             {"gauge", "count", "Dependency span count per rollup window, not a cumulative counter.", false},
	"trace_internal_call_graph_count":    {"gauge", "count", "Internal dependency span count per rollup window, not a cumulative counter.", false},
	"trace_service_apdex_score":          {"gauge", "ratio", "Service Apdex score for a rollup window.", false},
	"trace_endpoint_apdex_score":         {"gauge", "ratio", "Endpoint Apdex score for a rollup window.", false},
	"last9_change_events":                {"gauge", "", "Change-event values and default scheduled-search log aggregates. Units depend on the event or configured aggregate; values are not cumulative counters.", false},
}

func Last9(name string) (Last9Definition, bool) {
	d, ok := last9Metrics[name]
	return d, ok
}
