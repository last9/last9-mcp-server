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
		case "/repos/last9/repo/contents/CHANGELOG.md":
			if got := r.Header.Get("Accept"); got != "application/vnd.github.raw+json" {
				t.Errorf("Accept = %q", got)
			}
			if r.URL.Query().Get("ref") != "abc123" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte("## [Unreleased]\n- Base entry (#1).\n"))
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

func TestGitHubFileAt(t *testing.T) {
	got, err := fakeGitHub(t).FileAt(context.Background(), "CHANGELOG.md", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "## [Unreleased]\n- Base entry (#1).\n" {
		t.Fatalf("FileAt = %q", got)
	}
	if _, err := fakeGitHub(t).FileAt(context.Background(), "CHANGELOG.md", "missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want one naming the 404", err)
	}
}

func TestReadPullRequestEvent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := dir + "/" + name
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	pr, err := ReadPullRequestEvent(write("pr.json", `{"pull_request":{"number":30,"base":{"sha":"abc123"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.Number != 30 || pr.BaseSHA != "abc123" {
		t.Fatalf("event = %+v, want #30 on abc123", pr)
	}

	if pr, err := ReadPullRequestEvent(write("push.json", `{"ref":"refs/heads/main"}`)); err != nil || pr != nil {
		t.Fatalf("push event = %+v, %v; want nil, nil", pr, err)
	}
	if _, err := ReadPullRequestEvent(write("bad.json", `{"pull_request":{"number":30}}`)); err == nil {
		t.Fatal("want an error for a pull_request event without a base sha")
	}
	if _, err := ReadPullRequestEvent(dir + "/absent.json"); err == nil {
		t.Fatal("want an error for a missing event file")
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

	// A subtest, so the workflow's -run filter for this test picks it up.
	t.Run("added bullets cite this pull request", func(t *testing.T) {
		eventPath := os.Getenv("GITHUB_EVENT_PATH")
		if eventPath == "" {
			t.Skip("GITHUB_EVENT_PATH not set")
		}
		pr, err := ReadPullRequestEvent(eventPath)
		if err != nil {
			t.Fatal(err)
		}
		if pr == nil {
			t.Skip("not a pull_request event")
		}
		base, err := gh.FileAt(t.Context(), "CHANGELOG.md", pr.BaseSHA)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range CheckCitesPullRequest(base, string(text), pr.Number) {
			t.Error(p)
		}
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
