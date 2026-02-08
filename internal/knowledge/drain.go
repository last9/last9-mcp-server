package knowledge

import (
	"fmt"
	"strings"
	"sync"
)

// DrainTree implements a simplified Drain log parser
type DrainTree struct {
	Depth       int
	MaxChildren int
	Similarity  float64
	Root        *DrainNode
	Clusters    map[string]*LogCluster // TemplateID -> Cluster
	mu          sync.RWMutex
}

type DrainNode struct {
	Children  map[string]*DrainNode
	IsLeaf    bool
	ClusterID string
}

type LogCluster struct {
	ID        string
	Template  []string // Tokenized template with wildcards <*>
	RawLogs   CountMap
	Variables []string // Captured variables
}

type CountMap map[string]int

func NewDrainTree() *DrainTree {
	return &DrainTree{
		Depth:       4,
		MaxChildren: 100,
		Similarity:  0.5,
		Root:        &DrainNode{Children: make(map[string]*DrainNode)},
		Clusters:    make(map[string]*LogCluster),
	}
}

// Parse extracts a template and variables from a log line
func (d *DrainTree) Parse(logLine string) (string, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Preprocessing (Simple Tokenization)
	tokens := strings.Fields(logLine)
	if len(tokens) == 0 {
		return "", nil
	}

	// Navigation
	curr := d.Root
	pathDepth := d.Depth
	if len(tokens) < pathDepth {
		pathDepth = len(tokens)
	}

	// Level 1: Length
	lengthToken := fmt.Sprintf("%d", len(tokens))
	if _, ok := curr.Children[lengthToken]; !ok {
		curr.Children[lengthToken] = &DrainNode{Children: make(map[string]*DrainNode)}
	}
	curr = curr.Children[lengthToken]

	// Level 2..N: Traverse tokens
	for i := 0; i < pathDepth; i++ {
		token := tokens[i]

		// Exact Match?
		if next, ok := curr.Children[token]; ok {
			curr = next
			continue
		}

		// No exact match, try wildcard/numeric?
		// Simplified: If MaxChildren valid, add new branch
		if len(curr.Children) < d.MaxChildren {
			curr.Children[token] = &DrainNode{Children: make(map[string]*DrainNode)}
			curr = curr.Children[token]
		} else {
			// Fallback to specific catch-all group if we care,
			// for now let's just pick the "wildcard" child if exists or create one
			wildcard := "<*>"
			if _, ok := curr.Children[wildcard]; !ok {
				curr.Children[wildcard] = &DrainNode{Children: make(map[string]*DrainNode)}
			}
			curr = curr.Children[wildcard]
		}
	}

	// Leaf Node Logic
	// If Leaf exists, check similarity with existing cluster
	if curr.IsLeaf {
		cluster := d.Clusters[curr.ClusterID]
		// In full Drain, we traverse all clusters in this leaf group and find best match via similarity.
		// Simplified: We assume path leads to unique cluster for now (or single bucket).
		// Update Template
		newTemplate := d.updateTemplate(cluster.Template, tokens)
		cluster.Template = newTemplate
		return cluster.ID, d.extractVariables(newTemplate, tokens)
	}

	// New Cluster
	clusterID := fmt.Sprintf("C%d", len(d.Clusters)+1)
	cluster := &LogCluster{
		ID:       clusterID,
		Template: tokens, // Initially raw
		RawLogs:  make(CountMap),
	}
	d.Clusters[clusterID] = cluster
	curr.IsLeaf = true
	curr.ClusterID = clusterID

	return clusterID, nil
}

func (d *DrainTree) updateTemplate(existing []string, newTokens []string) []string {
	if len(existing) != len(newTokens) {
		return existing
	}
	updated := make([]string, len(existing))
	for i, t := range existing {
		if t == newTokens[i] {
			updated[i] = t
		} else {
			updated[i] = "<*>" // Generalize
		}
	}
	return updated
}

func (d *DrainTree) extractVariables(template []string, tokens []string) []string {
	if len(template) != len(tokens) {
		return nil
	}
	var vars []string
	for i, t := range template {
		if t == "<*>" {
			vars = append(vars, tokens[i])
		}
	}
	return vars
}

// GetTemplateString returns the string representation of a cluster
func (d *DrainTree) GetTemplateString(id string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if c, ok := d.Clusters[id]; ok {
		return strings.Join(c.Template, " ")
	}
	return ""
}
