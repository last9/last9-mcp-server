package utils

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Log search paths, recorded on the tool span so the server-side search and
// client-side chunking can be compared per call in production.
const (
	LogSearchPathAPI     = "log_search_api"
	LogSearchPathChunked = "chunked"
)

const (
	attrLogSearchPath     = attribute.Key("last9.logs.search_path")
	attrLogSearchStrategy = attribute.Key("last9.logs.search_strategy")
	attrLogUpstreamStatus = attribute.Key("last9.logs.upstream_status")

	attrLogSearchAttempts      = attribute.Key("last9.logs.search_attempts")
	attrLogSearchRetriedStatus = attribute.Key("last9.logs.search_retried_status")
)

// TagLogSearchPath records which search path served the call.
func TagLogSearchPath(ctx context.Context, path string) {
	trace.SpanFromContext(ctx).SetAttributes(attrLogSearchPath.String(path))
}

// TagLogUpstreamStatus records the HTTP status of a log query. Chunks share the
// tool span, so with several chunks the last one to answer wins.
func TagLogUpstreamStatus(ctx context.Context, status int) {
	trace.SpanFromContext(ctx).SetAttributes(attrLogUpstreamStatus.Int(status))
}

func tagLogSearchStrategy(ctx context.Context, result map[string]any) {
	stats, _ := result["search_stats"].(map[string]any)
	if strategy, ok := stats["strategy"].(string); ok && strategy != "" {
		trace.SpanFromContext(ctx).SetAttributes(attrLogSearchStrategy.String(strategy))
	}
}
