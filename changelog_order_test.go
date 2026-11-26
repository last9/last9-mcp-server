package main

import (
	"os/exec"
	"testing"
)

func TestPrioritizeChangelogScript(t *testing.T) {
	output, err := exec.Command("bash", "scripts/prioritize-changelog_test.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("prioritize changelog test: %v\n%s", err, output)
	}
}
