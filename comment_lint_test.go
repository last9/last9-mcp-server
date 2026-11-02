package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

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
