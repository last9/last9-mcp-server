package dashboards

const (
	ListDashboardsDescription = "List custom dashboards in the Last9 organization. Returns JSON array of dashboard summaries (id, name, metadata)."
	GetDashboardDescription   = "Get a custom dashboard by ID. Requires region (query param) for panel query population; defaults to configured datasource region."
	CreateDashboardDescription = "Create a custom dashboard. Body uses API envelope {dashboard, metadata}. Panels use queries[] with nested legend; metadata uses _category and _type."
	UpdateDashboardDescription = "Update a custom dashboard by ID. Same envelope as create. Readonly system dashboards return an error."
	DeleteDashboardDescription = "Delete a custom dashboard by ID. Readonly system dashboards cannot be deleted."
)
