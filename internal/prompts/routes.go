package prompts

import (
	"regexp"
	"strings"
)

// routingSections point a model at its next tool. Each of their lines is a
// "- " bullet whose backticked names are all tools.
var routingSections = map[string]bool{"When not to use": true, "Pairs well with": true}

var (
	sectionHeading = regexp.MustCompile(`^[ \t]*## (.+?)[ \t]*$`)
	backtickedName = regexp.MustCompile("`([^`]+)`")
)

// TrimRoutes drops each routing bullet that names a tool serves rejects, and
// any routing section left without a bullet, so a narrowed toolset never sends
// a model to a tool it does not serve.
func TrimRoutes(description string, serves func(tool string) bool) string {
	lines := strings.Split(description, "\n")
	out := make([]string, 0, len(lines))
	var pending []string
	routing, bullets := false, 0
	flush := func() {
		if bullets > 0 {
			out = append(out, pending...)
		}
		pending, bullets = nil, 0
	}
	for _, line := range lines {
		if m := sectionHeading.FindStringSubmatch(line); m != nil {
			flush()
			routing = routingSections[m[1]]
		}
		if !routing {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if !namesServed(line, serves) {
				continue
			}
			bullets++
		}
		pending = append(pending, line)
	}
	flush()
	return strings.Join(out, "\n")
}

func namesServed(line string, serves func(string) bool) bool {
	for _, m := range backtickedName.FindAllStringSubmatch(line, -1) {
		if !serves(m[1]) {
			return false
		}
	}
	return true
}
