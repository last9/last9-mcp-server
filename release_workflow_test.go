package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestHomebrewDispatchIsIndependentAndFailsOnHTTPError(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/release.yaml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	jobStarts := regexp.MustCompile(`(?m)^  [a-z][a-z0-9-]*:\n`).FindAllStringIndex(workflow, -1)
	jobs := make(map[string]string, len(jobStarts))
	for i, start := range jobStarts {
		end := len(workflow)
		if i+1 < len(jobStarts) {
			end = jobStarts[i+1][0]
		}
		name := strings.TrimSuffix(strings.TrimSpace(workflow[start[0]:start[1]]), ":")
		jobs[name] = workflow[start[0]:end]
	}
	dispatchJob := jobs["dispatch-homebrew-tap"]
	if dispatchJob == "" {
		t.Fatal("release workflow must define a dispatch-homebrew-tap job")
	}
	for _, want := range []string{
		"needs: tag-and-release",
		"if: needs.tag-and-release.outputs.version_changed == 'true'",
		"curl -fsS",
		"needs.tag-and-release.outputs.new_version",
	} {
		if !strings.Contains(dispatchJob, want) {
			t.Errorf("dispatch-homebrew-tap job missing %q", want)
		}
	}

	tagJob := jobs["tag-and-release"]
	if strings.Contains(tagJob, "Trigger homebrew-tap update workflow") {
		t.Error("tag-and-release must not dispatch homebrew-tap; its failure would block artifact publishing")
	}
}
