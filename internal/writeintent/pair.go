package writeintent

// Pair is one create/update couple for a single resource kind.
//
// PatchTool and a Gate.Allow hook are reserved for a future patch_* tool and
// validate-before-write; do not add them until a handler actually uses them.
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
