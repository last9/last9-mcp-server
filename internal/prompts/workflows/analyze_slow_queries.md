Workflow: analyze slow database queries{{if .db_system}} on {{.db_system}}{{end}}{{if .host}} (host {{.host}}){{end}} over {{.time}}.

Steps:
1. Call get_database_slow_queries ({{if .db_system}}db_system={{.db_system}}{{else}}no db_system filter, so it surveys all database systems{{end}}{{if .host}}, host={{.host}}{{end}}) over {{.time}} to rank the slowest queries.
2. {{if not .db_system}}From step 1, pick the database system behind the slowest queries, then {{end}}call get_database_server_metrics ({{if .db_system}}db_system={{.db_system}}{{else}}db_system = the system identified in step 1{{end}}) over {{.time}} to check whether server-side pressure (connections, memory, disk) explains the slowness.

Report the slowest query pattern and whether it is query-shape-bound or server-resource-bound. Map {{.time}} to each tool's lookback minutes or absolute range.
