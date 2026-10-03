package toolsets

const (
	manageGrantMarker = "\x00pulse_manage_explicit"
	allToolsMarker    = "\x00all_tools"
)

// ManageOnly returns the registration view for managed tools. A non-nil empty
// set fails closed when pulse_manage was not explicitly selected.
func ManageOnly(allowed Set) Set {
	if allowed == nil {
		return Set{}
	}
	if _, granted := allowed[manageGrantMarker]; !granted {
		return Set{}
	}
	return allowed
}

func addManageGrant(allowed Set) {
	allowed[manageGrantMarker] = struct{}{}
}

func addAllMarker(allowed Set) {
	allowed[allToolsMarker] = struct{}{}
}
