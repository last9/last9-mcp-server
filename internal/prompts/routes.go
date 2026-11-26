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

// Section is one "## " heading of a description and the text under it.
type Section struct {
	Name string
	Body string
}

// Sections splits description at its "## " headings, in order; text before
// the first heading belongs to no section.
func Sections(description string) []Section {
	var sections []Section
	var body []string
	flush := func() {
		if len(sections) > 0 {
			sections[len(sections)-1].Body = strings.Join(body, "\n")
		}
		body = nil
	}
	for _, line := range strings.Split(description, "\n") {
		if m := sectionHeading.FindStringSubmatch(line); m != nil {
			flush()
			sections = append(sections, Section{Name: m[1]})
			continue
		}
		body = append(body, line)
	}
	flush()
	return sections
}

// IsRoutingSection reports whether the section named name may hold only
// "- " bullets naming tools.
func IsRoutingSection(name string) bool {
	return routingSections[name]
}

// RouteNames returns the backticked names in text, in order.
func RouteNames(text string) []string {
	var names []string
	for _, m := range backtickedName.FindAllStringSubmatch(text, -1) {
		names = append(names, m[1])
	}
	return names
}

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
			routing = IsRoutingSection(m[1])
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
	for _, name := range RouteNames(line) {
		if !serves(name) {
			return false
		}
	}
	return true
}
