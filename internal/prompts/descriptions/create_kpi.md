Create a KPI on an existing alert group. Create once, keep the returned id, and bind
it in create_alert / validate_alert_expression; do not create a duplicate for the same
query.

Before calling: verify the PromQL returns data with prometheus_range_query, and choose
a unit that matches the threshold you will set (seconds vs milliseconds, ratio vs
percent — unit mistakes produce wrong thresholds).

Input:
- entity_id (required): alert group/entity UUID
- name (required): human-readable name; often used as the indicator name in expressions
- query (required): PromQL expression
- unit (required): unit of the query result (e.g. seconds, ratio, bytes)

Returns the created KPI id, name, query, and unit. Use get_entity_kpis to list KPIs
already on the group. For expression grammar and static vs adaptive field rules, read
last9://reference/alerting.
