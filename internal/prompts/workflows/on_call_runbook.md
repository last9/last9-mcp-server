Workflow: on-call triage for symptom "{{.symptom}}"{{if .service}} on {{.service}}{{end}}{{if .env}} (env "{{.env}}"){{end}} over {{.time}}.

{{if not .service}}No service given, so start with fleet triage: call get_alerts to find firing alerts, then get_service_summary as a volume leaderboard (unless sort_by names an error class) to identify high-traffic services, and scope the branch below to the worst offender. Once the target service is identified, call get_service_profile(service_name=<service>) before symptom routing.
{{else}}Call get_service_profile(service_name={{.service}}) before symptom routing. Use the result for all routing below — do not re-derive telemetry shape via PromQL or attribute probing. Pick env from profile.deployment.envs{{if not .env}}; if multiple or empty, call get_service_environments{{end}}.
{{end}}Route by symptom (honor profile.telemetry — skip trace/log tools when absent):
{{if eq .symptom "latency"}}Latency: get_service_performance_details, then get_apm_service_deviations, then get_service_traces only if telemetry.traces == "present", then get_service_dependency_graph. Decide whether the slowness is local or downstream.
{{else if eq .symptom "errors"}}Errors: get_service_performance_details, then get_exceptions, then get_service_traces only if telemetry.traces == "present". For HTTP 5xx/4xx log search use get_service_logs (http_status_class or http_status_code)—do not write logjson. For severity-less logger counts, aggregate get_logs only if telemetry.logs == "present" (count by logger; use profile.level_field when severity_set is none or partial; gate on ERROR/FATAL) before reading raw lines.
{{else if eq .symptom "database"}}Database: get_database_slow_queries, then get_database_server_metrics. Separate query-shape problems from server-resource pressure.
{{else}}Unknown symptom: call get_alerts first to see what is actually firing, then follow the matching branch above (latency, errors, or database).
{{end}}
Report the single most likely root cause and the one signal that supports it. Map {{.time}} to each tool's own time window.
