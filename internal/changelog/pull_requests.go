package changelog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/issues/%d", g.APIURL, g.Repo, number), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+g.Token)
	resp, err := g.Client.Do(req)
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
