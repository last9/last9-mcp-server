package utils

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var rawBodyErrorf = regexp.MustCompile(`Errorf\(.*string\((body|respBody|bodyBytes)\)`)

func TestNoRawUpstreamBodyInErrors(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if rawBodyErrorf.MatchString(line) {
				t.Errorf("%s:%d interpolates a raw body into an error; use utils.NewUpstreamHTTPError or utils.SanitizeUpstreamBody", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
