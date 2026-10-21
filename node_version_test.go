package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// patchedNodeReleases is the first release of each supported Node line that
// rejects TLS chains through the compromised eMudhra intermediates
// (nodejs/node#66871; CHANGELOG_V22/V24/V26 entries 4342503fd8, 01acf0ee74,
// a2caddd20e). The npm postinstall downloads the release binary with Node's
// https module, so every Node surface must be at or above one of these.
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
