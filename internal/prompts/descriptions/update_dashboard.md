Update a custom dashboard by ID. Same {dashboard, metadata} envelope as create_dashboard (full replacement). Readonly system dashboards return an error.

Prefer this tool for panel, layout, or query changes after create. Pass the existing dashboard id; do not call create_dashboard to refine a dashboard that already has an id.
