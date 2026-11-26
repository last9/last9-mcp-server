package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const longCommentBaseline = "testdata/long_comment_baseline.txt"

func TestNoNewLongCommentBlocks(t *testing.T) {
	found, err := repoLongComments(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(longCommentBaseline)
	if err != nil {
		t.Fatal(err)
	}
	baseline := map[string]int{}
	for _, entry := range strings.Split(string(raw), "\n") {
		if entry != "" {
			baseline[entry]++
		}
	}
	var added []string
	for _, c := range found {
		if baseline[c.baselineEntry()] > 0 {
			baseline[c.baselineEntry()]--
			continue
		}
		added = append(added, fmt.Sprintf("%s:%d: %d-line comment block", c.path, c.line, c.lines))
	}
	if len(added) > 0 {
		t.Errorf("comment blocks over %d lines (AGENTS.md, Comments): keep the why to 3 lines, or fix the code instead\n%s",
			maxCommentLines, strings.Join(added, "\n"))
	}
	var stale []string
	for entry, n := range baseline {
		for range n {
			stale = append(stale, entry)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%s lists blocks that are gone or changed; delete these lines:\n%s", longCommentBaseline, strings.Join(stale, "\n"))
	}
}

func TestLongCommentBlocksFixtures(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []longComment
	}{
		{"three lines", "package p\n// a\n// b\n// c\nvar x int\n", nil},
		{"four lines", "package p\n// a\n// b\n// c\n// d\nvar x int\n", []longComment{{"p.go", 2, 4, "// a"}}},
		{"blank line splits blocks", "package p\n// a\n// b\n\n// c\n// d\nvar x int\n", nil},
		{"directives not counted", "package p\n// a\n// b\n// c\n//go:embed x.md\nvar x string\n", nil},
		{"block comment lines", "package p\n/* a\nb\nc\nd */\nvar x int\n", []longComment{{"p.go", 2, 4, "/* a"}}},
		{"function body", "package p\nfunc f() {\n\t// a\n\t// b\n\t// c\n\t// d\n\t_ = 1\n}\n", []longComment{{"p.go", 3, 4, "// a"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "p.go", tc.src, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			if got := longCommentBlocks(fset, "p.go", file); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

const maxCommentLines = 3

type longComment struct {
	path      string
	line      int
	lines     int
	firstLine string
}

func (c longComment) baselineEntry() string {
	return fmt.Sprintf("%s\t%d\t%s", c.path, c.lines, c.firstLine)
}

func repoLongComments(root string) ([]longComment, error) {
	var long []longComment
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		long = append(long, longCommentBlocks(fset, filepath.ToSlash(path), file)...)
		return nil
	})
	return long, err
}

func longCommentBlocks(fset *token.FileSet, path string, file *ast.File) []longComment {
	var long []longComment
	for _, group := range file.Comments {
		lines := 0
		for _, c := range group.List {
			if !isDirective(c.Text) {
				lines += strings.Count(c.Text, "\n") + 1
			}
		}
		if lines > maxCommentLines {
			first, _, _ := strings.Cut(group.List[0].Text, "\n")
			long = append(long, longComment{path, fset.Position(group.Pos()).Line, lines, strings.TrimSpace(first)})
		}
	}
	return long
}

func isDirective(text string) bool {
	return strings.HasPrefix(text, "//go:") || strings.HasPrefix(text, "//line ") || strings.HasPrefix(text, "//nolint")
}
