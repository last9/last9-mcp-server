package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPrioritizeChangelogScript(t *testing.T) {
	output, err := exec.Command("bash", "scripts/prioritize-changelog_test.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("prioritize changelog test: %v\n%s", err, output)
	}
}

func TestPullRequestChecksChangelogOrder(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "scripts/prioritize-changelog.sh ${{ github.event.pull_request.number }}") {
		t.Fatal("pull request workflow does not check changelog order")
	}
}
