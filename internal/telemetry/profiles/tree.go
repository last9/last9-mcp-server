package profiles

import (
	"sort"
	"strings"
)

type trieNode struct {
	value    float64
	self     float64
	children map[string]*trieNode
}

func newTrieNode() *trieNode {
	return &trieNode{children: make(map[string]*trieNode)}
}

// BuildFlameTree derives a nested flamegraph from flat stack rows.
// Frames are stored leaf→root in Body; we reverse to root→leaf for the tree.
func BuildFlameTree(rows []FlamegraphRow) FlameNode {
	root := newTrieNode()
	for _, row := range rows {
		if row.Frames == "" || row.Samples == 0 {
			continue
		}
		frames := strings.Split(row.Frames, FrameDelimiter)
		// reverse leaf→root to root→leaf
		for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
			frames[i], frames[j] = frames[j], frames[i]
		}

		root.value += row.Samples
		node := root
		for idx, frame := range frames {
			child, ok := node.children[frame]
			if !ok {
				child = newTrieNode()
				node.children[frame] = child
			}
			child.value += row.Samples
			if idx == len(frames)-1 {
				child.self += row.Samples
			}
			node = child
		}
	}
	return toFlameNode("root", root)
}

func toFlameNode(name string, node *trieNode) FlameNode {
	children := make([]FlameNode, 0, len(node.children))
	for childName, child := range node.children {
		children = append(children, toFlameNode(childName, child))
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].Value != children[j].Value {
			return children[i].Value > children[j].Value
		}
		return children[i].Name < children[j].Name
	})
	return FlameNode{
		Name:     name,
		Value:    node.value,
		Self:     node.self,
		Children: children,
	}
}

// FoldToTopFunctions aggregates self/total samples per frame name.
func FoldToTopFunctions(rows []FlamegraphRow) []TopFunction {
	type counts struct {
		total float64
		self  float64
	}
	totals := make(map[string]*counts)

	for _, row := range rows {
		if row.Frames == "" || row.Samples == 0 {
			continue
		}
		frames := strings.Split(row.Frames, FrameDelimiter)
		leaf := frames[0]

		seen := make(map[string]struct{}, len(frames))
		for _, frame := range frames {
			if _, ok := seen[frame]; ok {
				continue
			}
			seen[frame] = struct{}{}
			entry, ok := totals[frame]
			if !ok {
				entry = &counts{}
				totals[frame] = entry
			}
			entry.total += row.Samples
			if frame == leaf {
				entry.self += row.Samples
			}
		}
	}

	out := make([]TopFunction, 0, len(totals))
	var profileTotal float64
	for name, c := range totals {
		profileTotal += c.self
		out = append(out, TopFunction{
			Name:         name,
			TotalSamples: c.total,
			SelfSamples:  c.self,
		})
	}
	for i := range out {
		if profileTotal > 0 {
			out[i].SelfPercent = (out[i].SelfSamples / profileTotal) * 100
			out[i].TotalPercent = (out[i].TotalSamples / profileTotal) * 100
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SelfSamples != out[j].SelfSamples {
			return out[i].SelfSamples > out[j].SelfSamples
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func getProfileTotalSamples(functions []TopFunction) float64 {
	var total float64
	for _, fn := range functions {
		total += fn.SelfSamples
	}
	return total
}

func limitTopFunctions(functions []TopFunction, limit int) []TopFunction {
	if limit <= 0 || limit >= len(functions) {
		return functions
	}
	return functions[:limit]
}
