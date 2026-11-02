package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// patchedNodeReleases are the first releases per line that reject the compromised
// eMudhra intermediates (nodejs/node#66871); postinstall downloads the binary over
// Node's https, so every Node surface must be at or above one of these.
var patchedNodeReleases = []string{"22.23.4", "24.22.1", "26.12.1"}

func TestPackageEnginesFloorEachNodeLineAtPatchedRelease(t *testing.T) {
	raw, err := os.ReadFile("package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}

	// Caret ranges keep each LTS line at or above its fix; the newest line is
	// open-ended so later majors install without a package bump.
	last := len(patchedNodeReleases) - 1
	ranges := make([]string, 0, len(patchedNodeReleases))
	for _, v := range patchedNodeReleases[:last] {
		ranges = append(ranges, "^"+v)
	}
	ranges = append(ranges, ">="+patchedNodeReleases[last])
	want := strings.Join(ranges, " || ")

	if pkg.Engines.Node != want {
		t.Errorf("package.json engines.node = %q, want %q", pkg.Engines.Node, want)
	}
}

// A bare major ('24') resolves to whatever is newest on the runner and may
// predate the fix, so workflows must pin an exact patched release.
func TestWorkflowsPinPatchedNodeRelease(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.y*ml")
	if err != nil {
		t.Fatal(err)
	}
	nodeVersion := regexp.MustCompile(`node-version:\s*['"]?([^'"\s#]+)`)
	pins := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range nodeVersion.FindAllStringSubmatch(string(raw), -1) {
			pins++
			if !slices.Contains(patchedNodeReleases, m[1]) {
				t.Errorf("%s pins node-version %q, want one of %v", f, m[1], patchedNodeReleases)
			}
		}
	}
	if pins == 0 {
		t.Fatal("no node-version pin found in .github/workflows; the npm publish job must pin one")
	}
}

func TestReadmeStatesPatchedNodeFloors(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "Node.js " + strings.Join(patchedNodeReleases[:len(patchedNodeReleases)-1], "+, ") +
		"+ or " + patchedNodeReleases[len(patchedNodeReleases)-1] + "+"
	if !strings.Contains(string(raw), want) {
		t.Errorf("README.md does not state the Node requirement %q", want)
	}
}
