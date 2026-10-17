package prompts

import "testing"

func TestTrimRoutes(t *testing.T) {
	const description = "Intro names `get_traces`.\n\n" +
		"\t## When to use\n\t- Logs: `get_traces`\n\n" +
		"\t## When not to use\n\t- Raw lines: `get_service_logs`\n\t- Ranking: `get_service_summary`\n\n" +
		"\t## Pairs well with\n\t- `get_traces` and `get_logs` (context)\n\n" +
		"\t## Output shape\n\tRows."
	tests := []struct {
		name   string
		served []string
		want   string
	}{
		{
			name:   "every tool served",
			served: []string{"get_logs", "get_traces", "get_service_logs", "get_service_summary"},
			want:   description,
		},
		{
			name:   "one route unserved",
			served: []string{"get_logs", "get_traces", "get_service_logs"},
			want: "Intro names `get_traces`.\n\n" +
				"\t## When to use\n\t- Logs: `get_traces`\n\n" +
				"\t## When not to use\n\t- Raw lines: `get_service_logs`\n\n" +
				"\t## Pairs well with\n\t- `get_traces` and `get_logs` (context)\n\n" +
				"\t## Output shape\n\tRows.",
		},
		{
			name:   "one of two tools in a bullet unserved, and a whole section",
			served: []string{"get_logs"},
			want: "Intro names `get_traces`.\n\n" +
				"\t## When to use\n\t- Logs: `get_traces`\n\n" +
				"\t## Output shape\n\tRows.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served := map[string]bool{}
			for _, name := range tt.served {
				served[name] = true
			}
			got := TrimRoutes(description, func(tool string) bool { return served[tool] })
			if got != tt.want {
				t.Fatalf("TrimRoutes =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}
