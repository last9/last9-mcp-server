package dashboards

import (
	"errors"
	"reflect"
)

type previewPanelRef struct {
	ID    string `json:"id,omitempty"`
	Index int    `json:"index"`
}

type previewDiff struct {
	PanelsAdded   []previewPanelRef `json:"panels_added"`
	PanelsChanged []previewPanelRef `json:"panels_changed"`
	PanelsRemoved []previewPanelRef `json:"panels_removed"`
	Changes       []patchChange     `json:"changes"`
}

func checkPreviewPanelIDs(dashboard map[string]any) error {
	seen := map[string]bool{}
	for _, raw := range asAnySlice(dashboard["panels"]) {
		panel, _ := raw.(map[string]any)
		id, _ := panel["id"].(string)
		if id == "" || seen[id] {
			return errors.New("saved panels must have distinct, nonempty ids for a patch preview")
		}
		seen[id] = true
	}
	return nil
}

func previewPanelDiff(before, after map[string]any, changes []patchChange) *previewDiff {
	diff := &previewDiff{PanelsAdded: []previewPanelRef{}, PanelsChanged: []previewPanelRef{}, PanelsRemoved: []previewPanelRef{}, Changes: changes}
	oldPanels := asAnySlice(before["panels"])
	newPanels := asAnySlice(after["panels"])
	for index, raw := range newPanels {
		panel, _ := raw.(map[string]any)
		id, _ := panel["id"].(string)
		ref := previewPanelRef{ID: id, Index: index}
		oldIndex := -1
		if id != "" {
			oldIndex = findPanel(oldPanels, id)
		}
		if oldIndex < 0 {
			diff.PanelsAdded = append(diff.PanelsAdded, ref)
		} else if oldIndex != index || !reflect.DeepEqual(oldPanels[oldIndex], panel) {
			diff.PanelsChanged = append(diff.PanelsChanged, ref)
		}
	}
	for index, raw := range oldPanels {
		panel, _ := raw.(map[string]any)
		id, _ := panel["id"].(string)
		if findPanel(newPanels, id) < 0 {
			diff.PanelsRemoved = append(diff.PanelsRemoved, previewPanelRef{ID: id, Index: index})
		}
	}
	return diff
}
