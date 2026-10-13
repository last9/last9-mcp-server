Update a custom dashboard by ID. Same `{dashboard, metadata}` envelope and same panel/query shape as create_dashboard (full replacement): panels use `version`, `layout`, `visualization`, and `queries`; each query uses `name`, `type`, `query_type`, `telemetry`, `expr`, and `legend: {type, value}`. Readonly system dashboards return an error.

Prefer this tool for panel, layout, or query changes after create. Pass the existing dashboard id; do not call create_dashboard to refine a dashboard that already has an id.
