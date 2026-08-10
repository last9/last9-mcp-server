package traces

import "testing"

func TestEnrichAttribute(t *testing.T) {
	tests := []struct {
		raw          string
		wantType     string
		wantField    string
		wantSemantic string
	}{
		// Priority 1: service.name aliases → ServiceName
		{"resource_service.name", "toplevel", "ServiceName", "service.name"},
		{"service.name", "toplevel", "ServiceName", "service.name"},
		// Priority 2: resource_* → resources['stripped']
		{"resource_k8s.cluster", "resource", "resources['k8s.cluster']", "k8s.cluster"},
		{"resource_department", "resource", "resources['department']", "department"},
		// Priority 3: event_* → events['stripped']
		{"event_exception.type", "event", "events['exception.type']", "exception.type"},
		{"event_message", "event", "events['message']", "message"},
		// Priority 4: known top-level fields
		{"ServiceName", "toplevel", "ServiceName", "ServiceName"},
		{"Duration", "toplevel", "Duration", "Duration"},
		{"StatusCode", "toplevel", "StatusCode", "StatusCode"},
		{"SpanKind", "toplevel", "SpanKind", "SpanKind"},
		// Priority 5: grpc.status_code OTel rename
		{"grpc.status_code", "span", "attributes['rpc.grpc.status_code']", "grpc.status_code"},
		// Priority 6: span attributes (catch-all)
		{"http.method", "span", "attributes['http.method']", "http.method"},
		{"db.statement", "span", "attributes['db.statement']", "db.statement"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := enrichAttribute(tt.raw)
			if got.Name != tt.raw {
				t.Errorf("Name = %q, want %q", got.Name, tt.raw)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.FilterField != tt.wantField {
				t.Errorf("FilterField = %q, want %q", got.FilterField, tt.wantField)
			}
			if got.SemanticName != tt.wantSemantic {
				t.Errorf("SemanticName = %q, want %q", got.SemanticName, tt.wantSemantic)
			}
		})
	}
}

func TestSpanAttributeField(t *testing.T) {
	if got := SpanAttributeField("db.system"); got != `attributes['db.system']` {
		t.Fatalf("SpanAttributeField(db.system) = %q, want attributes['db.system']", got)
	}
}

func TestResourceAttributeField(t *testing.T) {
	tests := []struct{ key, want string }{
		{"deployment.environment", `resources['deployment.environment']`},
		// Flat API form must not double-prefix into resources['resource_...'].
		{"resource_deployment.environment", `resources['deployment.environment']`},
		{"service.name", "ServiceName"},
		{"resource_service.name", "ServiceName"},
	}
	for _, tt := range tests {
		if got := ResourceAttributeField(tt.key); got != tt.want {
			t.Errorf("ResourceAttributeField(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestEventAttributeField(t *testing.T) {
	tests := []struct{ key, want string }{
		{"exception.type", `events['exception.type']`},
		{"event_exception.type", `events['exception.type']`},
	}
	for _, tt := range tests {
		if got := EventAttributeField(tt.key); got != tt.want {
			t.Errorf("EventAttributeField(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestNormalizeTagName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"resources['department']", "resource_department"},
		{"events['exception.type']", "event_exception.type"},
		{"attributes['http.method']", "http.method"},
		{"resource_department", "resource_department"},
		{"event_exception.type", "event_exception.type"},
		{"http.method", "http.method"},
		{"ServiceName", "ServiceName"},
		{"  resources['k8s.cluster']  ", "resource_k8s.cluster"},
		// empty key edge case: wrapping chars only → no stripping (len guard fails)
		{"resources['']", "resources['']"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeTagName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeTagName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
