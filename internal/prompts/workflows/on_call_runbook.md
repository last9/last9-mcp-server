Workflow: on-call triage for symptom "{{.symptom}}"{{if .service}} on {{.service}}{{end}}{{if .env}} (env "{{.env}}"){{end}} over {{.time}}.

{{if not .service}}No service given, so start with fleet triage: call get_alerts to find firing alerts, then get_service_summary as a volume leaderboard (unless sort_by names an error class) to identify high-traffic services, and scope the branch below to the worst offender.
{{end}}Route by symptom:
{{if eq .symptom "latency"}}Latency: get_service_performance_details, then get_apm_service_deviations, then get_service_traces, then get_service_dependency_graph. Decide whether the slowness is local or downstream.
{{else if eq .symptom "errors"}}Errors: get_service_performance_details, then get_exceptions, then get_service_traces, then aggregate get_logs (count by logger, gate on ERROR/FATAL) before reading raw lines.
{{else if eq .symptom "database"}}Database: get_database_slow_queries, then get_database_server_metrics. Separate query-shape problems from server-resource pressure.
{{else}}Unknown symptom: call get_alerts first to see what is actually firing, then follow the matching branch above (latency, errors, or database).
{{end}}
Report the single most likely root cause and the one signal that supports it. Map {{.time}} to each tool's own time window.
