package main

import "testing"

func TestServerVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{name: "release", version: "0.24.0", commit: "abc1234", want: "0.24.0"},
		{name: "development commit", version: "dev", commit: "abc1234567890", want: "dev+abc1234"},
		{name: "development unknown commit", version: "dev", commit: "unknown", want: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverVersion(tt.version, tt.commit); got != tt.want {
				t.Fatalf("serverVersion(%q, %q) = %q, want %q", tt.version, tt.commit, got, tt.want)
			}
		})
	}
}
