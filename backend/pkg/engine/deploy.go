package engine

import (
	"context"
	"log"
	"os"

	"backend/pkg/models"
)

// DeployRoomVFS deploys the room's VFS to Cloudflare Pages. It takes a
// read-only snapshot of the VFS (Rule 1: non-blocking to WS loops), then
// runs the upload in a background Goroutine, broadcasting deployment
// status events to all connected clients.
//
// The roomID is implicit (r.chatID); the method is safe to call from any
// Goroutine.
func (r *ProjectRoom) DeployRoomVFS(ctx context.Context) {
	if r.cf == nil {
		log.Printf("[room:%s] no Cloudflare client configured; skipping deploy", r.chatID)
		r.BroadcastMessage(models.DeploymentFailed{
			Type:  "deployment_failed",
			Error: "Cloudflare client is not configured (set CLOUDFLARE_ACCOUNT_ID / CLOUDFLARE_API_TOKEN)",
		})
		return
	}

	// Snapshot the VFS into a map[string][]byte (read-only copy).
	snapshot := r.snapshotVFSBytes()
	if len(snapshot) == 0 {
		log.Printf("[room:%s] VFS is empty; nothing to deploy", r.chatID)
		r.BroadcastMessage(models.DeploymentFailed{
			Type:  "deployment_failed",
			Error: "VFS is empty; nothing to deploy",
		})
		return
	}

	// Run the deployment asynchronously so the WS loops are never blocked.
	go r.runDeployment(ctx, snapshot)
}

// snapshotVFSBytes returns a deep copy of the VFS as map[string][]byte.
func (r *ProjectRoom) snapshotVFSBytes() map[string][]byte {
	r.vfsMu.RLock()
	defer r.vfsMu.RUnlock()

	out := make(map[string][]byte, len(r.vfs))
	for path, entry := range r.vfs {
		cp := make([]byte, len(entry.FileContents))
		copy(cp, entry.FileContents)
		out[path] = cp
	}
	return out
}

// runDeployment drives the Cloudflare Pages upload and broadcasts status.
func (r *ProjectRoom) runDeployment(ctx context.Context, snapshot map[string][]byte) {
	// Broadcast deployment start (existing protocol event the frontend handles).
	r.BroadcastMessage(models.DeploymentStarted{
		Type:    "deployment_started",
		Message: "Deployment started",
		Files:   filePaths(snapshot),
	})

	// Progress: packaging done, uploading.
	r.BroadcastMessage(models.DeployProgress{
		Type:     "deploy_progress",
		Message:  "Packaging project files",
		Progress: 25,
	})

	projectName := r.deployProjectName()
	result, err := r.cf.UploadToPages(ctx, projectName, snapshot)
	if err != nil {
		log.Printf("[room:%s] deploy failed: %v", r.chatID, err)
		r.BroadcastMessage(models.DeploymentFailed{
			Type:  "deployment_failed",
			Error: err.Error(),
		})
		return
	}

	// Progress: upload complete.
	r.BroadcastMessage(models.DeployProgress{
		Type:     "deploy_progress",
		Message:  "Upload complete, finalizing",
		Progress: 90,
	})

	// Broadcast completion with the live preview URL.
	r.BroadcastMessage(models.DeploymentCompleted{
		Type:       "deployment_completed",
		PreviewURL: result.URL,
		TunnelURL:  "",
		InstanceID: result.DeploymentID,
		Message:    "Deployment complete",
	})

	r.BroadcastMessage(models.DeployProgress{
		Type:     "deploy_progress",
		Message:  "Deployment complete",
		Progress: 100,
	})

	log.Printf("[room:%s] deployed to %s (deployment %s)", r.chatID, result.URL, result.DeploymentID)
}

// deployProjectName returns the Cloudflare Pages project name for this
// room, defaulting to a slug derived from the chat ID.
func (r *ProjectRoom) deployProjectName() string {
	if name := deployProjectNameOverride(); name != "" {
		return name
	}
	if name := os.Getenv("CLOUDFLARE_PAGES_PROJECT"); name != "" {
		return name
	}
	return "vibesdk-" + r.chatID
}

// filePaths extracts the sorted list of file paths from a snapshot.
func filePaths(snapshot map[string][]byte) []struct {
	FilePath string `json:"filePath"`
} {
	paths := make([]struct {
		FilePath string `json:"filePath"`
	}, 0, len(snapshot))
	for p := range snapshot {
		paths = append(paths, struct {
			FilePath string `json:"filePath"`
		}{FilePath: p})
	}
	return paths
}

// deployProjectNameOverride is a hook for tests to override the project
// name without touching env. Defaults to empty (use chat-ID slug).
var deployProjectNameOverride = func() string { return "" }
