package github

import (
	"context"
	"fmt"
	"net/http"
)

// CreateRelease creates a new release
func (c *Client) CreateRelease(ctx context.Context, owner, repo string, input *ReleaseInput) (*Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases", owner, repo)
	var result Release
	if err := c.doRequest(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateGitTag creates a lightweight git tag via the GitHub API.
// This creates only the tag ref, not a GitHub Release — letting GoReleaser
// handle the full release creation with binary assets on tag push.
func (c *Client) CreateGitTag(ctx context.Context, owner, repo, tag, sha string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo)
	body := map[string]string{
		"ref": "refs/tags/" + tag,
		"sha": sha,
	}
	return c.doRequest(ctx, http.MethodPost, path, body, nil)
}

// GetLatestRelease gets the latest published release
// Returns nil, nil if no releases exist
func (c *Client) GetLatestRelease(ctx context.Context, owner, repo string) (*Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo)
	var result Release
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		// 404 means no releases exist - return nil, nil
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

// GetReleaseByTag fetches a release by its tag name.
// Returns nil, nil if no release exists for the given tag (404).
func (c *Client) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (*Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	var result Release
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

// UpdateRelease updates an existing release (e.g. to enrich the body with a summary).
func (c *Client) UpdateRelease(ctx context.Context, owner, repo string, releaseID int64, input *ReleaseInput) (*Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/%d", owner, repo, releaseID)
	var result Release
	if err := c.doRequest(ctx, http.MethodPatch, path, input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListReleases lists releases for a repository (newest first)
func (c *Client) ListReleases(ctx context.Context, owner, repo string, perPage int) ([]*Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases?per_page=%d", owner, repo, perPage)
	var result []*Release
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListTags lists repository tags (newest first)
func (c *Client) ListTags(ctx context.Context, owner, repo string, perPage int) ([]*Tag, error) {
	path := fmt.Sprintf("/repos/%s/%s/tags?per_page=%d", owner, repo, perPage)
	var result []*Tag
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetTagForSHA returns the tag name if a tag exists at the given SHA, or empty string if none.
// Used to detect if a commit has already been tagged (race condition prevention).
func (c *Client) GetTagForSHA(ctx context.Context, owner, repo, sha string) (string, error) {
	tags, err := c.ListTags(ctx, owner, repo, 20)
	if err != nil {
		return "", err
	}
	for _, tag := range tags {
		if tag.Commit.SHA == sha {
			return tag.Name, nil
		}
	}
	return "", nil
}
