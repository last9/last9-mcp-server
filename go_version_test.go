package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// patchedGoRelease is the toolchain every build surface pins: Go 1.27.3 is the
// first 1.27 release whose crypto/x509 rejects the compromised eMudhra
// intermediates, and CI and release read it from go.mod via go-version-file.
const patchedGoRelease = "1.27.3"

func TestGoModPinsPatchedGoRelease(t *testing.T) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go\s+(\S+)\s*$`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("go.mod has no go directive")
	}
	if got := string(m[1]); got != patchedGoRelease {
		t.Errorf("go.mod go directive = %q, want %q", got, patchedGoRelease)
	}
}

func TestDockerfileBuildsWithPatchedGoAndUpgradesCABundle(t *testing.T) {
	raw, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(raw)

	images := regexp.MustCompile(`(?m)^FROM\s+(?:--platform=\S+\s+)?golang:(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	if len(images) == 0 {
		t.Fatal("Dockerfile has no golang builder stage")
	}
	for _, m := range images {
		if want := patchedGoRelease + "-alpine"; m[1] != want {
			t.Errorf("Dockerfile builds with golang:%s, want golang:%s", m[1], want)
		}
	}

	// alpine:latest can lag the CA bundle that distrusts the intermediates, so the
	// runtime stage must upgrade it rather than keep whatever the base image has.
	stages := strings.Split(dockerfile, "\nFROM ")
	runtime := stages[len(stages)-1]
	if want := "apk add --no-cache --upgrade ca-certificates ca-certificates-bundle"; !strings.Contains(runtime, want) {
		t.Errorf("Dockerfile runtime stage does not run %q", want)
	}
}

func TestReadmeStatesPatchedGoRelease(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	// Go 1.26.10 carries the same fix, so the README must not call every release
	// before the project minimum vulnerable.
	want := "Requires Go " + patchedGoRelease + " or later, which includes the eMudhra intermediate-certificate distrust fix."
	if !strings.Contains(string(raw), want) {
		t.Errorf("README.md does not state the Go requirement %q", want)
	}
}
