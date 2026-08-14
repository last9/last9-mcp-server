package profiles

// ProfileType is the continuous-profiling sample kind (PDE-718 / ENG-1699).
type ProfileType string

const (
	ProfileTypeCPU   ProfileType = "cpu"
	ProfileTypeAlloc ProfileType = "alloc"
	ProfileTypeWall  ProfileType = "wall"

	DefaultProfileType = ProfileTypeCPU

	// DefaultLookbackMinutes matches the Profiling UI default (1 hour).
	DefaultLookbackMinutes = 60

	// DefaultFlamegraphRowLimit matches FLAMEGRAPH_ROW_LIMIT in the dashboard client.
	DefaultFlamegraphRowLimit = 1000

	// MaxFlamegraphRowLimit matches the PDE-718 query_range hard cap (~10000).
	MaxFlamegraphRowLimit = 10000

	// DefaultTopFunctionsLimit is the post-fold ranking cap for get_top_functions.
	DefaultTopFunctionsLimit = 50

	// FrameDelimiter separates stack frames in Body / Frames (0x1f).
	FrameDelimiter = "\x1f"
)

// Resource field paths used in filter/aggregate pipelines (ProfilesApis PROFILE_RESOURCE_FIELDS).
const (
	ResourceEnv       = "resources['deployment.environment.name']"
	ResourceCluster   = "resources['k8s.cluster.name']"
	ResourceNamespace = "resources['k8s.namespace.name']"
	ResourceRuntime   = "resources['telemetry.sdk.language']"
)

// ProfileFilters scopes a profiles query_range/json pipeline.
type ProfileFilters struct {
	Service     string
	Env         string
	Cluster     string
	Namespace   string
	Runtime     string
	ProfileType ProfileType
}

// FlamegraphRow is one aggregated stack from the flamegraph dataframe.
type FlamegraphRow struct {
	StackHash string  `json:"stack_hash"`
	Frames    string  `json:"frames"`
	Samples   float64 `json:"samples"`
}

// FlameNode is the nested flamegraph tree returned to agents.
type FlameNode struct {
	Name     string      `json:"name"`
	Value    float64     `json:"value"`
	Self     float64     `json:"self"`
	Children []FlameNode `json:"children,omitempty"`
}

// TopFunction is a ranked function with self/total sample counts.
type TopFunction struct {
	Name         string  `json:"name"`
	TotalSamples float64 `json:"total_samples"`
	SelfSamples  float64 `json:"self_samples"`
	SelfPercent  float64 `json:"self_percent"`
	TotalPercent float64 `json:"total_percent"`
}

// ProfileServiceIndexRow is one landing-table service with relative sample share.
type ProfileServiceIndexRow struct {
	Name          string  `json:"name"`
	Samples       float64 `json:"samples"`
	Share         float64 `json:"share"`
	SharePercent  float64 `json:"share_percent"`
	Runtime       string  `json:"runtime,omitempty"`
	LastProfileAt string  `json:"last_profile_at,omitempty"`
}
