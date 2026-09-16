package grafana

import (
	"encoding/json"
	"sort"
)

// SearchHit is one dashboard row from GET /api/search.
type SearchHit struct {
	ID    int      `json:"id"`
	UID   string   `json:"uid"`
	Title string   `json:"title"`
	URI   string   `json:"uri,omitempty"`
	URL   string   `json:"url,omitempty"`
	Type  string   `json:"type"`
	Tags  []string `json:"tags,omitempty"`
}

// Folder is one row from GET /api/folders.
type Folder struct {
	ID   int    `json:"id"`
	UID  string `json:"uid"`
	Name string `json:"title"`
	URL  string `json:"url,omitempty"`
}

// Datasource is the safe projection of GET /api/datasources. Credential
// fields (user, password, basicAuthUser, secureJsonFields) are never included.
type Datasource struct {
	ID         int    `json:"id"`
	UID        string `json:"uid,omitempty"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	TypeName   string `json:"typeName,omitempty"`
	URL        string `json:"url,omitempty"`
	Access     string `json:"access,omitempty"`
	IsDefault  bool   `json:"isDefault"`
	IsReadOnly bool   `json:"isReadOnly,omitempty"`
}

// DashboardResponse is the body of GET /api/dashboards/uid/{uid}. Panels,
// templating and targets carry types flexible enough to accept real Grafana
// payloads (datasource as string or {uid,type}, targets that may be
// arbitrary JSON), so the raw HTTP response decodes without data loss.
type DashboardResponse struct {
	Dashboard Dashboard `json:"dashboard"`
}

type Dashboard struct {
	ID            int         `json:"id,omitempty"`
	UID           string      `json:"uid"`
	Title         string      `json:"title"`
	Tags          []string    `json:"tags,omitempty"`
	Timezone      string      `json:"timezone,omitempty"`
	SchemaVersion int         `json:"schemaVersion,omitempty"`
	Version       int         `json:"version,omitempty"`
	Templating    *Templating `json:"templating,omitempty"`
	Panels        []Panel     `json:"panels,omitempty"`
}

type Templating struct {
	List []TemplateVariable `json:"list,omitempty"`
}

type TemplateVariable struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Query      json.RawMessage `json:"query,omitempty"`
	Datasource json.RawMessage `json:"datasource,omitempty"`
	Hide       int             `json:"hide,omitempty"`
}

type Panel struct {
	ID         int             `json:"id"`
	Title      string          `json:"title,omitempty"`
	Type       string          `json:"type"`
	Datasource json.RawMessage `json:"datasource,omitempty"`
	GridPos    GridPos         `json:"gridPos,omitempty"`
	Targets    []Target        `json:"targets,omitempty"`
	Panels     []Panel         `json:"panels,omitempty"`
}

type GridPos struct {
	H int `json:"h,omitempty"`
	W int `json:"w,omitempty"`
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`
}

type Target struct {
	RefID      string          `json:"refId,omitempty"`
	Datasource json.RawMessage `json:"datasource,omitempty"`
	Expr       string          `json:"expr,omitempty"`
	Query      json.RawMessage `json:"query,omitempty"`
	RawQuery   json.RawMessage `json:"rawQuery,omitempty"`
	QueryType  string          `json:"queryType,omitempty"`
}

// DashboardSummary is the filtered view served by grafana_get_dashboard by
// default: every panel, its datasource ref, and the PromQL expr per target.
// Panels whose type is not query-known surface their raw target JSON instead of
// a fabricated summary, so no query is silently dropped.
type DashboardSummary struct {
	UID        string         `json:"uid"`
	Title      string         `json:"title"`
	Tags       []string       `json:"tags,omitempty"`
	Version    int            `json:"version,omitempty"`
	Timezone   string         `json:"timezone,omitempty"`
	Templating []TemplateVar  `json:"templating,omitempty"`
	Panels     []PanelSummary `json:"panels"`
	// UnsupportedPanelTypes lists panel types outside the known set; their
	// panels still appear with type+datasource in Panels so nothing is dropped.
	UnsupportedPanelTypes []string `json:"unsupportedPanelTypes,omitempty"`
}

type TemplateVar struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Query      string `json:"query,omitempty"`
	Datasource string `json:"datasource,omitempty"`
	Hide       int    `json:"hide,omitempty"`
}

type PanelSummary struct {
	ID         int             `json:"id"`
	Title      string          `json:"title,omitempty"`
	Type       string          `json:"type"`
	Datasource string          `json:"datasource,omitempty"`
	GridPos    GridPos         `json:"gridPos,omitempty"`
	Targets    []TargetSummary `json:"targets,omitempty"`
}

type TargetSummary struct {
	RefID      string `json:"refId,omitempty"`
	Expr       string `json:"expr,omitempty"`
	QueryType  string `json:"queryType,omitempty"`
	Datasource string `json:"datasource,omitempty"`
}

// Known query panel types. Panels of any other type render as "rowCount-n
// additional panels" so a plugin panel's JSON survives without us modeling it.
func knownPanelTypes() map[string]bool {
	return map[string]bool{
		"graph": true, "timeseries": true, "table": true, "stat": true,
		"gauge": true, "bargauge": true, "piechart": true, "heatmap": true,
		"logs": true, "trace": true, "state-timeline": true, "status-history": true,
		"candlestick": true, "trend": true,
	}
}

// datasourceRef reduces Grafana's flexible datasource field (a bare uid string,
// or {"type":..., "uid":...}) to a single stable string: "type/uid". Empty when
// the panel inherits the default datasource.
func datasourceRef(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Type string `json:"type"`
		UID  string `json:"uid"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		switch {
		case obj.Type != "" && obj.UID != "":
			return obj.Type + "/" + obj.UID
		case obj.Type != "":
			return obj.Type
		default:
			return obj.UID
		}
	}
	return string(raw)
}

// summarizeTarget reduces a target to its PromQL surface. Targets without an
// expr (non-Prometheus datasources, plugin queries) still surface their refId
// and datasource so the caller knows a query exists but cannot be summarized.
func summarizeTarget(t Target) TargetSummary {
	out := TargetSummary{
		RefID:      t.RefID,
		Datasource: datasourceRef(t.Datasource),
		Expr:       t.Expr,
		QueryType:  t.QueryType,
	}
	return out
}

// summarizePanel flattens one panel; nested panels (a row's children) are
// summarized in place with their index preserved in the JSON array order.
func summarizePanel(p Panel) PanelSummary {
	sum := PanelSummary{
		ID:         p.ID,
		Title:      p.Title,
		Type:       p.Type,
		Datasource: datasourceRef(p.Datasource),
		GridPos:    p.GridPos,
	}
	for _, t := range p.Targets {
		sum.Targets = append(sum.Targets, summarizeTarget(t))
	}
	return sum
}

// summarizeDashboard builds the filtered view. It walks top-level panels and
// flattens row panels' children into the same list; panels of unknown types are
// still included (their type + datasource identify them) rather than dropped.
func summarizeDashboard(d Dashboard) DashboardSummary {
	sum := DashboardSummary{
		UID:      d.UID,
		Title:    d.Title,
		Tags:     d.Tags,
		Version:  d.Version,
		Timezone: d.Timezone,
	}
	if d.Templating != nil {
		for _, tv := range d.Templating.List {
			sum.Templating = append(sum.Templating, TemplateVar{
				Name:       tv.Name,
				Type:       tv.Type,
				Query:      string(tv.Query),
				Datasource: datasourceRef(tv.Datasource),
				Hide:       tv.Hide,
			})
		}
	}
	known := knownPanelTypes()
	unsupported := make(map[string]bool)
	var walk func([]Panel)
	walk = func(panels []Panel) {
		for _, p := range panels {
			if p.Type == "row" {
				walk(p.Panels)
				continue
			}
			if !known[p.Type] {
				unsupported[p.Type] = true
			}
			sum.Panels = append(sum.Panels, summarizePanel(p))
		}
	}
	walk(d.Panels)
	for t := range unsupported {
		sum.UnsupportedPanelTypes = append(sum.UnsupportedPanelTypes, t)
	}
	sort.Strings(sum.UnsupportedPanelTypes)
	return sum
}
