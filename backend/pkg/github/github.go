package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the GitHub REST API (Contents API) to push files to a
// repository. It avoids a full git client by using the Contents API, which
// is sufficient for pushing a flat set of generated files.
type Client struct {
	token string
	hc    *http.Client
}

// NewClient creates a GitHub API client.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		hc:    &http.Client{Timeout: 60 * time.Second},
	}
}

// RepoInfo holds the owner and repo name parsed from a GitHub URL.
type RepoInfo struct {
	Owner string
	Repo  string
}

// ExtractRepoInfo parses "https://github.com/owner/repo" or
// "git@github.com:owner/repo.git" into owner/repo.
func ExtractRepoInfo(url string) (*RepoInfo, error) {
	clean := url
	if strings.HasPrefix(url, "git@github.com:") {
		clean = "https://github.com/" + strings.TrimPrefix(url, "git@github.com:")
	}
	clean = strings.TrimSuffix(clean, ".git")
	clean = strings.TrimSuffix(clean, "/")

	// https://github.com/owner/repo
	trimmed := strings.TrimPrefix(clean, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid GitHub URL: %s", url)
	}
	return &RepoInfo{Owner: parts[0], Repo: parts[1]}, nil
}

// CreateRepository creates a new GitHub repository for the authenticated user.
func (c *Client) CreateRepository(ctx context.Context, name, description string, isPrivate bool) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"name":        name,
		"description": description,
		"private":     isPrivate,
		"auto_init":   true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/user/repos", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		// Repository already exists — return a sentinel error.
		return "", fmt.Errorf("repository already exists")
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create repo failed (status %d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.HTMLURL, nil
}

// PushFile writes a single file to the repository via the Contents API.
// If the file already exists, it updates it (requires the current SHA).
func (c *Client) PushFile(ctx context.Context, owner, repo, path, content string) error {
	// Check if file exists to get its SHA.
	sha := ""
	getURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	getReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	getReq.Header.Set("Authorization", "Bearer "+c.token)
	getReq.Header.Set("Accept", "application/vnd.github+json")
	getResp, err := c.hc.Do(getReq)
	if err == nil {
		defer getResp.Body.Close()
		if getResp.StatusCode == http.StatusOK {
			var existing struct {
				SHA string `json:"sha"`
			}
			_ = json.NewDecoder(getResp.Body).Decode(&existing)
			sha = existing.SHA
		}
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message": "Update " + path,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"sha":     sha, // empty for new files
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, getURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push file %s failed (status %d): %s", path, resp.StatusCode, string(b))
	}
	return nil
}

// PushFiles pushes a map of file paths to contents, plus a GitHub Actions
// workflow that builds and deploys to Cloudflare Pages.
func (c *Client) PushFiles(ctx context.Context, owner, repo string, files map[string]string) error {
	// Add the GitHub Actions workflow for build + deploy to Cloudflare Pages.
	files[".github/workflows/deploy.yml"] = cloudflarePagesWorkflow

	for path, content := range files {
		if err := c.PushFile(ctx, owner, repo, path, content); err != nil {
			return err
		}
	}
	return nil
}

// cloudflarePagesWorkflow is a GitHub Actions workflow that builds the
// generated app and deploys the static output to Cloudflare Pages.
const cloudflarePagesWorkflow = `name: Deploy to Cloudflare Pages

on:
  push:
    branches: [main]

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      deployments: write
    steps:
      - uses: actions/checkout@v4

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: 20

      - name: Install dependencies
        run: npm ci || npm install

      - name: Build
        run: npm run build

      - name: Deploy to Cloudflare Pages
        uses: cloudflare/wrangler-action@v3
        with:
          apiToken: ${{ secrets.CLOUDFLARE_API_TOKEN }}
          accountId: ${{ secrets.CLOUDFLARE_ACCOUNT_ID }}
          command: pages deploy dist --project-name ${{ vars.CLOUDFLARE_PAGES_PROJECT }}
`
