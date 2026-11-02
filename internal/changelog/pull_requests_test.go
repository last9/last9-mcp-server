package changelog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func fakeGitHub(t *testing.T) GitHub {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/repos/last9/repo/issues/12":
			_, _ = w.Write([]byte(`{"number":12,"pull_request":{"url":"x"}}`))
		case "/repos/last9/repo/issues/481":
			_, _ = w.Write([]byte(`{"number":481}`))
		case "/repos/last9/repo/issues/500":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return GitHub{Client: srv.Client(), APIURL: srv.URL, Repo: "last9/repo", Token: "tok"}
}

func TestGitHubIsPullRequest(t *testing.T) {
	gh := fakeGitHub(t)
	for n, want := range map[int]bool{12: true, 481: false, 999: false} {
		got, err := gh.IsPullRequest(context.Background(), n)
		if err != nil {
			t.Fatalf("#%d: %v", n, err)
		}
		if got != want {
			t.Errorf("IsPullRequest(#%d) = %v, want %v", n, got, want)
		}
	}
}

func TestGitHubIsPullRequestFailsOnUnexpectedStatus(t *testing.T) {
	_, err := fakeGitHub(t).IsPullRequest(context.Background(), 500)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want one naming the 502", err)
	}
}

func TestCheckReportsIssueCitations(t *testing.T) {
	text := "## [Unreleased]\n### Fixed\n- Fixed it (#481).\n- Fixed more (#12, #481).\n- Fixed again (#999).\n"
	problems, err := Check(context.Background(), text, fakeGitHub(t).IsPullRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 ||
		!strings.HasPrefix(problems[0], "#481 is not a pull request") ||
		!strings.HasPrefix(problems[1], "#999 is not a pull request") {
		t.Fatalf("problems = %q, want #481 and #999 once each", problems)
	}
}

func TestCheckStopsOnResolveError(t *testing.T) {
	_, err := Check(context.Background(), "## [Unreleased]\n- Broke (#500).\n", fakeGitHub(t).IsPullRequest)
	if err == nil || !strings.Contains(err.Error(), "#500") {
		t.Fatalf("err = %v, want one naming #500", err)
	}
}

// TestRepoChangelogCitesPullRequests resolves every number CHANGELOG.md's
// [Unreleased] bullets cite. CI runs it with GITHUB_TOKEN on pull requests.
func TestRepoChangelogCitesPullRequests(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set")
	}
	text, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	gh := GitHub{
		Client: http.DefaultClient,
		APIURL: envOr("GITHUB_API_URL", "https://api.github.com"),
		Repo:   envOr("GITHUB_REPOSITORY", "last9/last9-mcp-server"),
		Token:  token,
	}
	problems, err := Check(context.Background(), string(text), gh.IsPullRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
