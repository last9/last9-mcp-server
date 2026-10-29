package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintTestGoroutines(t *testing.T) {
	output, err := lintTestGoroutines(t.Context(), ".")
	if err != nil {
		t.Fatalf("test goroutine lint: %v\n%s\nWorkers must return errors for assertions in the parent goroutine.", err, output)
	}
}

func TestLintTestGoroutinesFixtures(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"test fatal", `go func() { t.Fatal("failure") }()`, "(*testing.T).Fatal"},
		{"test fatalf", `go func() { t.Fatalf("%s", "failure") }()`, "(*testing.T).Fatalf"},
		{"test failnow", `go func() { t.FailNow() }()`, "(*testing.T).FailNow"},
		{"benchmark fatal", `go func() { b.Fatal("failure") }()`, "(*testing.B).Fatal"},
		{"benchmark fatalf", `go func() { b.Fatalf("%s", "failure") }()`, "(*testing.B).Fatalf"},
		{"benchmark failnow", `go func() { b.FailNow() }()`, "(*testing.B).FailNow"},
		{"nested worker", `go func() { go func() { t.FailNow() }() }()`, "(*testing.T).FailNow"},
		{"aliased receiver", `workerT := t; go func() { workerT.FailNow() }()`, "(*testing.T).FailNow"},
		{"parent assertion", `errs := make(chan error, 1); go func() { errs <- fmt.Errorf("failure") }(); if err := <-errs; err != nil { t.Fatal(err) }`, ""},
		{"subtest assertion", `t.Run("child", func(t *testing.T) { t.Fatal("failure") })`, ""},
		{"nonfatal worker", `go func() { t.Error("failure"); b.Errorf("%s", "failure") }()`, ""},
		{"unrelated method", `other := unrelated{}; go func() { other.Fatal("message") }()`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := fmt.Sprintf(`package fixture
import ("testing"; "fmt")
var _ = fmt.Errorf
type unrelated struct{}
func (unrelated) Fatal(string) {}
func check(t *testing.T, b *testing.B) { %s }
`, tc.body)
			for name, content := range map[string]string{
				"go.mod":         "module fixture\n\ngo 1.25\n",
				"worker_test.go": source,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			output, err := lintTestGoroutines(t.Context(), dir)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("safe fixture rejected: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(string(output), "call to "+tc.want+" from a non-test goroutine") || !strings.Contains(string(output), "worker_test.go:") {
				t.Fatalf("want diagnostic for %s at worker_test.go, got error %v:\n%s", tc.want, err, output)
			}
		})
	}
}

func lintTestGoroutines(ctx context.Context, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", "vet", "-testinggoroutine", "./...")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestLintTestGoroutinesCI(t *testing.T) {
	for _, workflow := range []string{"test.yaml", "release.yaml"} {
		t.Run(workflow, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(".github", "workflows", workflow))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "\n        run: go vet -testinggoroutine ./...\n") {
				t.Fatal("workflow must run go vet -testinggoroutine ./... independently of the test cache")
			}
		})
	}
}

func TestLintTestGoroutinesAfterCachedPass(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "probe"), 0700); err != nil {
		t.Fatal(err)
	}
	lintTests, err := os.ReadFile("test_lint_test.go")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.25\n")
	write("test_lint_test.go", string(lintTests))
	probe := filepath.Join("internal", "probe", "worker_test.go")
	safe := "package probe\nimport \"testing\"\nfunc check(t *testing.T) { go func() { t.Error(\"probe\") }() }\n"
	write(probe, safe)
	runTests := func() string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "go", "test", ".", "./internal/probe", "-run", "^TestLintTestGoroutines$")
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture tests failed: %v\n%s", err, output)
		}
		return string(output)
	}
	runTests()
	if output := runTests(); !strings.Contains(output, "(cached)") {
		t.Fatalf("fixture did not warm the test cache:\n%s", output)
	}
	write(probe, strings.Replace(safe, "t.Error", "t.Fatal", 1))
	// Subprocess source reads do not invalidate the root package's test cache.
	if output := runTests(); !strings.Contains(output, "(cached)") {
		t.Fatalf("expected cached root success after editing an internal test:\n%s", output)
	}
	output, err := lintTestGoroutines(t.Context(), dir)
	if err == nil || !strings.Contains(string(output), "worker_test.go:") ||
		!strings.Contains(string(output), "call to (*testing.T).Fatal from a non-test goroutine") {
		t.Fatalf("explicit lint must reject the edited worker despite cached tests: %v\n%s", err, output)
	}
	write(probe, safe)
	if output, err := lintTestGoroutines(t.Context(), dir); err != nil {
		t.Fatalf("repaired worker rejected: %v\n%s", err, output)
	}
}
