package traces

// traceTopLevelFields is the set of first-class trace fields that are used
// directly by name in tracejson filter conditions (no bracket syntax needed).
var traceTopLevelFields = map[string]struct{}{
	"TraceId": {}, "SpanId": {}, "ServiceName": {}, "SpanName": {},
	"SpanKind": {}, "StatusCode": {}, "StatusMessage": {}, "Duration": {},
	"Timestamp": {}, "ParentSpanId": {}, "TraceState": {},
}

// enrichAttribute converts a raw attribute name from the traces series API into
// a TraceAttribute with an LLM-ready filter_field and usage hint.
//
// Priority order:
//  1. resource_service.name / service.name  → ServiceName (top-level special case)
//  2. resource_* prefix                     → resources['<stripped>']
//  3. event_* prefix                        → events['<stripped>']
//  4. Known top-level fields                → use as-is
//  5. grpc.status_code                      → attributes['rpc.grpc.status_code']
//  6. Everything else                       → attributes['<raw>']
func enrichAttribute(raw string) TraceAttribute {
	// Priority 1
	if raw == "resource_service.name" || raw == "service.name" {
		return TraceAttribute{
			Name:         raw,
			SemanticName: "service.name",
			Type:         "toplevel",
			FilterField:  "ServiceName",
			Hint:         `Example: {"$eq": ["ServiceName", "checkout"]}`,
		}
	}

	// Priority 2: resource_* → resources['<stripped>']
	if len(raw) > 9 && raw[:9] == "resource_" {
		stripped := raw[9:]
		filterField := "resources['" + stripped + "']"
		return TraceAttribute{
			Name:         raw,
			SemanticName: stripped,
			Type:         "resource",
			FilterField:  filterField,
			Hint:         `Example: {"$eq": ["` + filterField + `", "value"]}`,
		}
	}

	// Priority 3: event_* → events['<stripped>']
	if len(raw) > 6 && raw[:6] == "event_" {
		stripped := raw[6:]
		filterField := "events['" + stripped + "']"
		return TraceAttribute{
			Name:         raw,
			SemanticName: stripped,
			Type:         "event",
			FilterField:  filterField,
			Hint:         `Example: {"$eq": ["` + filterField + `", "value"]}`,
		}
	}

	// Priority 4: known top-level fields
	if _, ok := traceTopLevelFields[raw]; ok {
		return TraceAttribute{
			Name:         raw,
			SemanticName: raw,
			Type:         "toplevel",
			FilterField:  raw,
			Hint:         `Example: {"$eq": ["` + raw + `", "value"]}`,
		}
	}

	// Priority 5: grpc.status_code OTel rename
	if raw == "grpc.status_code" {
		return TraceAttribute{
			Name:         raw,
			SemanticName: raw,
			Type:         "span",
			FilterField:  "attributes['rpc.grpc.status_code']",
			Hint:         `Example: {"$eq": ["attributes['rpc.grpc.status_code']", "0"]}`,
		}
	}

	// Priority 6: span attributes
	filterField := "attributes['" + raw + "']"
	return TraceAttribute{
		Name:         raw,
		SemanticName: raw,
		Type:         "span",
		FilterField:  filterField,
		Hint:         `Example: {"$eq": ["` + filterField + `", "value"]}`,
	}
}

// normalizeTagName converts a filter_field syntax string or raw API tag name
// into the raw API tag name expected by the tag-values endpoint.
//
//	resources['department']    → resource_department
//	events['exception.type']   → event_exception.type
//	attributes['http.method']  → http.method
//	resource_department        → resource_department  (pass-through)
func normalizeTagName(input string) string {
	t := trimSpace(input)
	if hasWrapping(t, "resources['", "']") {
		return "resource_" + t[len("resources['"):len(t)-2]
	}
	if hasWrapping(t, "events['", "']") {
		return "event_" + t[len("events['"):len(t)-2]
	}
	if hasWrapping(t, "attributes['", "']") {
		return t[len("attributes['") : len(t)-2]
	}
	return t
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func hasWrapping(s, prefix, suffix string) bool {
	return len(s) > len(prefix)+len(suffix) &&
		s[:len(prefix)] == prefix &&
		s[len(s)-len(suffix):] == suffix
}
