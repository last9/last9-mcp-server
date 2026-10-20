Workflow: investigate a latency spike in {{.service}}{{if .env}} (env "{{.env}}"){{end}} over {{.time}}.

Steps:
0. Call get_service_profile(service_name={{.service}}). Use the result for all routing below — do not re-derive telemetry shape via PromQL or attribute probing.
1. {{if not .env}}Pick env from profile.deployment.envs; if multiple or empty, call get_service_environments for {{.service}} and pick the affected one — then call {{else}}Call {{end}}get_service_performance_details (service_name={{.service}}{{if .env}}, env={{.env}}{{end}}) over {{.time}} to confirm the p99/p50 rise and locate the slow operation.
2. Call get_change_events (service_name={{.service}}) over {{.time}}; if a change precedes the spike, call analyze_change_impact with that event as change_event: `no significant change` rules the change out, so move on; `possible change` and `insufficient data` do not, so keep the change a suspect.
3. Call get_apm_service_deviations to find which attributes or operations deviate from baseline.
4. If telemetry.traces != "absent", call get_service_traces for representative slow traces of the deviating operation; otherwise skip trace tools.
5. Call get_service_dependency_graph to see whether the latency originates downstream.
6. If a downstream span errors and telemetry.traces != "absent", call get_exceptions on that service to confirm the root cause.

Report the slowest operation, whether it is local or downstream, and the specific signal that explains the spike. Map {{.time}} to each tool's own time window (lookback minutes or an absolute range).
