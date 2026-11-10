package profiles

import (
	"os"
	"strings"
	"testing"
)

func TestRunQueryRangeSanitizesOnlyErrorResponses(t *testing.T) {
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}

	statusCheck := strings.Index(string(src), "switch resp.StatusCode")
	sanitize := strings.Index(string(src), "utils.SanitizeUpstreamBody")
	if statusCheck < 0 || sanitize < 0 {
		t.Fatal("runQueryRange must check the response status and sanitize error bodies")
	}
	if sanitize < statusCheck {
		t.Fatal("runQueryRange sanitizes successful response bodies")
	}
}
