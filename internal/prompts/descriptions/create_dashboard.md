Create a custom dashboard. Body uses API envelope {dashboard, metadata}. Panels use queries[] with nested legend; metadata uses _category and _type.

Use this tool only for a net-new dashboard (no id yet). Create once. After this tool returns, keep dashboard.id and call update_dashboard for any panel, layout, or query change — including later in the same turn. If an id was returned this turn or is already known, do not call create_dashboard again.
