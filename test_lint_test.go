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
