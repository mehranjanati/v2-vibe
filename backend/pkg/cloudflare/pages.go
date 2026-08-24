package cloudflare

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"time"
)

// DeployResult is the outcome of a Pages Direct Upload deployment.
type DeployResult struct {
	DeploymentID string `json:"deploymentId"`
	URL          string `json:"url"` // live .pages.dev URL
	ProjectName  string `json:"projectName"`
	Environment  string `json:"environment"`
}

// Client talks to the Cloudflare API v4.
type Client struct {
	accountID string
	apiToken  string
	hc        *http.Client
}

// NewClient creates a Cloudflare API client.
func NewClient(accountID, apiToken string) *Client {
	return &Client{
		accountID: accountID,
		apiToken:  apiToken,
		hc:        &http.Client{Timeout: 60 * time.Second},
	}
}

// UploadToPages deploys a VFS snapshot to Cloudflare Pages via the
// Direct Upload API. The VFS is packaged as an in-memory zip (no temp
// files on disk) and sent as multipart form-data.
//
// Endpoint:
//
//	POST /accounts/{account_id}/pages/projects/{project_name}/deployments
func (c *Client) UploadToPages(ctx context.Context, projectName string, vfsMap map[string][]byte) (*DeployResult, error) {
	if c.accountID == "" || c.apiToken == "" {
		return nil, fmt.Errorf("cloudflare: accountID and apiToken are required")
	}
	if projectName == "" {
		return nil, fmt.Errorf("cloudflare: projectName is required")
	}

	// 1. Package the VFS into an in-memory zip.
	zipBytes, err := buildZip(vfsMap)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: package VFS: %w", err)
	}

	// 2. Build multipart form-data with the zip as the "file" field.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "project.zip")
	if err != nil {
		return nil, fmt.Errorf("cloudflare: create form file: %w", err)
	}
	if _, err := fw.Write(zipBytes); err != nil {
		return nil, fmt.Errorf("cloudflare: write form file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("cloudflare: close multipart writer: %w", err)
	}

	// 3. POST to the Pages deployments endpoint.
	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/pages/projects/%s/deployments",
		c.accountID, projectName,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: read response: %w", err)
	}

	// 4. Parse the JSON envelope.
	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Result *struct {
			ID          string `json:"id"`
			URL         string `json:"url"`
			Environment string `json:"environment"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("cloudflare: parse response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode != http.StatusOK || !envelope.Success {
		msg := "unknown error"
		if len(envelope.Errors) > 0 {
			msg = envelope.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare: deploy failed (status %d): %s", resp.StatusCode, msg)
	}

	if envelope.Result == nil {
		return nil, fmt.Errorf("cloudflare: deploy response missing result")
	}

	return &DeployResult{
		DeploymentID: envelope.Result.ID,
		URL:          envelope.Result.URL,
		ProjectName:  projectName,
		Environment:  envelope.Result.Environment,
	}, nil
}

// buildZip packages a map of file paths to contents into an in-memory zip.
// Paths are sorted for deterministic output.
func buildZip(vfsMap map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	paths := make([]string, 0, len(vfsMap))
	for p := range vfsMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		content := vfsMap[path]
		w, err := zw.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(content); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
