package profiles

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var eagerProfileSanitize = regexp.MustCompile(`(?m)^\s+\w+\s*:=\s*utils\.SanitizeUpstreamBody`)

func TestRunQueryRangeSanitizesOnlyErrorResponses(t *testing.T) {
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}

	code := string(src)
	statusCheck := strings.Index(code, "switch resp.StatusCode")
	sanitize := strings.Index(code, "utils.SanitizeUpstreamBody")
	if statusCheck < 0 || sanitize < 0 {
		t.Fatal("runQueryRange must check the response status and sanitize error bodies")
	}
	if eagerProfileSanitize.MatchString(code[:statusCheck]) {
		t.Fatal("runQueryRange sanitizes successful response bodies")
	}
}
