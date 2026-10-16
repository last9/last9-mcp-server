package alerting

import (
	"strings"
	"unicode"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createdViaMCP marks rules, channels and attachments this server creates so agents can
// tell their own resources apart. The API rejects any other created_via value.
const createdViaMCP = "last9-mcp"

// The leading underscore keeps these annotations out of alert notifications.
const (
	annotationCreatedVia       = "_l9_created_via_"
	annotationCreatedViaClient = "_l9_created_via_client_"
)

const maxCreatedViaClientRunes = 128

// createdViaClient names the MCP client that made req as "<name>/<version>", read from the
// session's clientInfo and never from tool arguments.
func createdViaClient(req *mcp.CallToolRequest) string {
	var name, version string
	if req != nil {
		if info := req.ClientInfo(); info != nil {
			name, version = info.Name, info.Version
		}
	}
	return sanitizeCreatedViaClient(clientPart(name) + "/" + clientPart(version))
}

func clientPart(s string) string {
	if s = sanitizeCreatedViaClient(s); strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

// sanitizeCreatedViaClient strips control characters and annotation template delimiters,
// then truncates. Removal repeats because deleting one pair can join another ("{}}{").
func sanitizeCreatedViaClient(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	for strings.Contains(s, "{{") || strings.Contains(s, "}}") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "{{", ""), "}}", "")
	}
	if r := []rune(s); len(r) > maxCreatedViaClientRunes {
		s = string(r[:maxCreatedViaClientRunes])
	}
	return s
}

// withCreatedViaAnnotations returns a copy of props whose annotations carry exactly the
// given marker: caller-supplied marker keys are dropped, then marker's are set. A nil or
// empty marker therefore strips any marker the caller tried to set.
func withCreatedViaAnnotations(props *AlertProperties, marker map[string]string) *AlertProperties {
	if props == nil && len(marker) == 0 {
		return nil
	}
	out := AlertProperties{}
	if props != nil {
		out = *props
	}
	annotations := make(map[string]string, len(out.Annotations)+len(marker))
	for k, v := range out.Annotations {
		if k != annotationCreatedVia && k != annotationCreatedViaClient {
			annotations[k] = v
		}
	}
	for k, v := range marker {
		annotations[k] = v
	}
	out.Annotations = annotations
	if len(out.Annotations) == 0 {
		out.Annotations = nil
	}
	return &out
}

func newCreatedViaFields(req *mcp.CallToolRequest) last9api.CreatedVia {
	return last9api.CreatedVia{CreatedVia: createdViaMCP, CreatedViaClient: createdViaClient(req)}
}

// storedCreatedViaMarker returns the marker annotations a stored rule already carries, so
// an edit that replaces properties keeps them.
func storedCreatedViaMarker(rule entities.AlertRule) map[string]string {
	marker := map[string]string{}
	for _, k := range []string{annotationCreatedVia, annotationCreatedViaClient} {
		if v, ok := rule.Properties.Annotations[k].(string); ok {
			marker[k] = v
		}
	}
	return marker
}

func newCreatedViaMarker(req *mcp.CallToolRequest) map[string]string {
	return map[string]string{
		annotationCreatedVia:       createdViaMCP,
		annotationCreatedViaClient: createdViaClient(req),
	}
}
