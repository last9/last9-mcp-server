# get_metrics Tool Usage Guide

Use for performance monitoring, trend analysis, and system health checks.

## When to Use
- Monitoring system performance trends
- Analyzing response time patterns
- Tracking error rates over time
- Identifying performance bottlenecks
- Comparing metrics across time periods
- Detecting anomalies and spikes

## Best Practices

### Time Range Selection
- For relative windows ("last 30 minutes", "past 2 hours"), prefer `lookback_minutes`.
- Use explicit `start_time_iso` / `end_time_iso` only when the user gives concrete timestamps.
- If both explicit timestamps and `lookback_minutes` are present, explicit timestamps take priority.
- **Recent monitoring**: Last 15-30 minutes
- **Trend analysis**: Last few hours to days
- **Performance baselines**: Week or month comparisons
- **Incident investigation**: Before, during, and after incident times

**ISO TIME FALLBACK RULE:**
- If you receive an error like "lookback_minutes cannot exceed..." or any lookback-related error,
  retry the same query using `start_time_iso` and `end_time_iso` parameters instead of `lookback_minutes`.
- Calculate the appropriate start and end timestamps in RFC3339 format (e.g. 2026-02-09T15:04:05Z)
  based on the user's requested time range, and reissue the tool call.

### Metric Selection
Choose metrics that align with the user's question:
- **Response times**: For performance issues
- **Error rates**: For reliability concerns
- **Throughput**: For capacity planning
- **Cache ratios**: For CDN optimization

## Common Use Cases

### Performance Analysis
- Response time trends
- Latency percentiles (p50, p95, p99)
- Throughput measurements
- Resource utilization

### Error Monitoring
- Error rate trends
- Error distribution by type
- Impact analysis

### Capacity Planning
- Traffic volume patterns
- Peak usage identification
- Growth trend analysis

## Tips
- Consider time zones when analyzing patterns
- Look for correlations between different metrics
- Use appropriate granularity for time ranges
- Compare against historical baselines when possible
