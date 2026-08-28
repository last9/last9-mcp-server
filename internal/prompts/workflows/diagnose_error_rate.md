Workflow: diagnose an elevated error rate in {{.service}}{{if .env}} (env "{{.env}}"){{end}} over {{.time}}.

Steps:
0. Call get_service_profile(service_name={{.service}}). Use the result for all routing below — do not re-derive telemetry shape via PromQL or attribute probing.
1. {{if not .env}}Pick env from profile.deployment.envs; if multiple or empty, call get_service_environments for {{.service}} and pick the affected one — then call {{else}}Call {{end}}get_service_performance_details (service_name={{.service}}{{if .env}}, env={{.env}}{{end}}) over {{.time}} to confirm the error-rate rise and find the failing operation.
2. Call get_exceptions for {{.service}} to identify the dominant exception type and its time bounds.
3. If telemetry.traces != "absent", call get_service_traces for representative failing traces of that operation; otherwise skip trace tools.
4. If telemetry.logs != "absent", AGGREGATE FIRST: call get_logs with a count pipeline using profile.signal_shape — use signal_shape.level_field when the profile reports one; when it does not, discover the field with get_log_attributes_for_pipeline instead of guessing a name (never SeverityText when severity_set is none or partial); if log_format is "json", add parse stage per signal_shape.parse_hint; gate on ERROR/FATAL using the level field; aggregate $count grouped by logger — before reading any raw lines. When telemetry.logs == "absent", skip log tools.
5. Call get_service_dependency_graph to check whether the errors originate from a downstream dependency.

Report the dominant error, whether it is local or downstream, and the log or trace signal that explains it. Map {{.time}} to each tool's own time window.
