package changelog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// GitHub resolves numbers through the REST issues endpoint, which serves both
// issues and pull requests and sets "pull_request" only on the latter.
type GitHub struct {
	Client *http.Client
	APIURL string
	Repo   string
	Token  string
}

// IsPullRequest calls GET /repos/{repo}/issues/{number}; a missing number is not a pull request.
func (g GitHub) IsPullRequest(ctx context.Context, number int) (bool, error) {
	resp, err := g.get(ctx, fmt.Sprintf("/repos/%s/issues/%d", g.Repo, number), "application/vnd.github+json")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var issue struct {
		PullRequest *json.RawMessage `json:"pull_request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return false, fmt.Errorf("decode GitHub issue: %w", err)
	}
	return issue.PullRequest != nil, nil
}

// FileAt calls GET /repos/{repo}/contents/{path}?ref={ref} for the file's raw bytes.
func (g GitHub) FileAt(ctx context.Context, path, ref string) (string, error) {
	resp, err := g.get(ctx, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", g.Repo, path, url.QueryEscape(ref)), "application/vnd.github.raw+json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s for %s at %s", resp.Status, path, ref)
	}
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func (g GitHub) get(ctx context.Context, path, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.APIURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+g.Token)
	return g.Client.Do(req)
}

// PullRequestEvent is the part of a GitHub Actions pull_request event payload the checks need.
type PullRequestEvent struct {
	Number  int
	BaseSHA string
}

// ReadPullRequestEvent reads the payload at GITHUB_EVENT_PATH; it returns nil for
// an event that is not about a pull request.
func ReadPullRequestEvent(path string) (*PullRequestEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var event struct {
		PullRequest *struct {
			Number int `json:"number"`
			Base   struct {
				SHA string `json:"sha"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("decode event payload: %w", err)
	}
	if event.PullRequest == nil {
		return nil, nil
	}
	if event.PullRequest.Number == 0 || event.PullRequest.Base.SHA == "" {
		return nil, errors.New("pull_request event payload has no number or base sha")
	}
	return &PullRequestEvent{Number: event.PullRequest.Number, BaseSHA: event.PullRequest.Base.SHA}, nil
}
