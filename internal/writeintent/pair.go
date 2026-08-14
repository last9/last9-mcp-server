package writeintent

// Kind is the write intent for a persistence pair.
type Kind string

const (
	// NetNew means the resource has no identity yet. The only legal write is the create tool.
	NetNew Kind = "net_new"
	// Refine means identity is known this turn or earlier. The only legal write is the update tool.
	Refine Kind = "refine"
)

// Pair is one create/update couple for a single resource kind.
//
// Handlers do not construct or enforce Kind this iteration (no hard block on a
// second create). PatchTool and a Gate.Allow hook are reserved for a future
// patch_* tool and validate-before-write; do not add them until product asks.
type Pair struct {
	Resource   string // "dashboard"
	CreateTool string // "create_dashboard"
	UpdateTool string // "update_dashboard"
	IDField    string // "id"
}

// Dashboard is the create_dashboard / update_dashboard write pair.
var Dashboard = Pair{
	Resource:   "dashboard",
	CreateTool: "create_dashboard",
	UpdateTool: "update_dashboard",
	IDField:    "id",
}
