package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{name: "release", version: "0.24.0", commit: "abc1234", want: "0.24.0"},
		{name: "development commit", version: "dev", commit: "abc1234567890", want: "dev+abc1234"},
		{name: "development empty commit", version: "dev", commit: "", want: "dev"},
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

func TestBuildServerAdvertisesDevelopmentVersion(t *testing.T) {
	originalVersion, originalCommit := Version, CommitSHA
	Version, CommitSHA = "dev", "abc1234567890"
	t.Cleanup(func() { Version, CommitSHA = originalVersion, originalCommit })

	server, err := buildServer(testToolRegistrationConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "version-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if got := session.InitializeResult().ServerInfo.Version; got != "dev+abc1234" {
		t.Fatalf("initialize serverInfo.version = %q, want %q", got, "dev+abc1234")
	}
}

func TestRunLogsDevelopmentVersion(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunLogsDevelopmentVersionHelper$")
	cmd.Env = append(os.Environ(), "LAST9_VERSION_TEST_HELPER=1", "LAST9_REFRESH_TOKEN=")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("helper succeeded, want configuration failure")
	}
	if got := string(output); !strings.Contains(got, "Starting Last9 MCP Server vdev+abc1234") {
		t.Fatalf("startup output = %q, want development version", got)
	}
}

func TestRunLogsDevelopmentVersionHelper(t *testing.T) {
	if os.Getenv("LAST9_VERSION_TEST_HELPER") != "1" {
		return
	}
	Version, CommitSHA = "dev", "abc1234567890"
	os.Args = []string{"last9-mcp"}
	run()
}
