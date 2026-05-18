package dashboards

const (
	ListDashboardsDescription = "List custom dashboards in the Last9 organization. Returns JSON array of dashboard summaries (id, name, metadata)."
	GetDashboardDescription   = "Get a custom dashboard by ID. Requires region (query param) for panel query population; defaults to configured datasource region."
	CreateDashboardDescription = "Create a custom dashboard. Body uses API envelope {dashboard, metadata}. Panels use queries[] with nested legend; metadata uses _category and _type."
	UpdateDashboardDescription = "Update a custom dashboard by ID. Same envelope as create. Readonly system dashboards return an error."
	DeleteDashboardDescription = "Delete a custom dashboard by ID. Readonly system dashboards cannot be deleted."

	ListDashboardTemplatesDescription = "List available dashboard templates that can be used with create_dashboard_from_template. Returns template IDs, display names, and required knob keys."
	CreateDashboardFromTemplateDescription = "Create a dashboard from an embedded template. Call list_dashboard_templates first to get a template_id and its required knobs (e.g. DASHBOARD_NAME, NAMESPACES, CLUSTERS, WINDOW). The template renders to a valid API JSON payload and POSTs to /dashboards/. Returns the created dashboard with a reference_url deep link."
)
