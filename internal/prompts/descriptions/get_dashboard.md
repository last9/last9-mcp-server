Get a custom dashboard by ID. Requires region (query param) for panel query population; defaults to configured datasource region.

The result adds top-level `locked` (`true` when the dashboard refuses edits and deletes, including built-in dashboards; `false` when the API omits it) and `created_by` (the author's user id, `null` if not returned).

To alert on a panel, see last9://reference/alerting.