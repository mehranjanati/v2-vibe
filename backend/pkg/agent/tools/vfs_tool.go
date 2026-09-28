package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// VFSStore is the read/write surface the VFS tools need against the
// Redis-backed virtual file system. *store.RedisCheckpointStore
// implements it.
type VFSStore interface {
	VFSWrite(ctx context.Context, chatID, path, content string) error
	VFSRead(ctx context.Context, chatID, path string) (string, bool, error)
	VFSDelete(ctx context.Context, chatID, path string) error
	VFSList(ctx context.Context, chatID string) ([]string, error)
}

// vfsToolContext carries per-session routing info (which chat's VFS to
// touch) that the LLM must NOT be able to control. Values are attached to
// the tool-call context by the engine before dispatch.
type vfsToolContext struct{}

type vfsCtxKey struct{}

// WithVFSContext returns a context tagged with the chat ID whose VFS the
// tools operate on.
func WithVFSContext(ctx context.Context, chatID string) context.Context {
	return context.WithValue(ctx, vfsCtxKey{}, chatID)
}

func vfsChatID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(vfsCtxKey{}).(string)
	if !ok || id == "" {
		return "", fmt.Errorf("tools: vfs: no chat context attached to tool call")
	}
	return id, nil
}

// --- vfs_write ---

// VFSWriteInput is the model-facing schema for writing a file.
type VFSWriteInput struct {
	Path    string `json:"path" jsonschema:"description=File path in the virtual file system, e.g. 'src/index.html'"`
	Content string `json:"content" jsonschema:"description=Full file contents to write"`
}

// VFSWriteOutput confirms the write.
type VFSWriteOutput struct {
	Written bool   `json:"written"`
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
}

// NewVFSWriteTool creates the "vfs_write" tool.
func NewVFSWriteTool(store VFSStore) (tool.InvokableTool, error) {
	return utils.InferTool("vfs_write",
		"Create or overwrite a file in the project's virtual file system. Always write the FULL file content.",
		func(ctx context.Context, in VFSWriteInput) (*VFSWriteOutput, error) {
			chatID, err := vfsChatID(ctx)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.Path) == "" {
				return nil, fmt.Errorf("tools: vfs_write: path is required")
			}
			if err := store.VFSWrite(ctx, chatID, in.Path, in.Content); err != nil {
				return nil, fmt.Errorf("tools: vfs_write: %w", err)
			}
			return &VFSWriteOutput{Written: true, Path: in.Path, Bytes: len(in.Content)}, nil
		})
}

// --- vfs_read ---

// VFSReadInput is the model-facing schema for reading a file.
type VFSReadInput struct {
	Path string `json:"path" jsonschema:"description=File path to read"`
}

// VFSReadOutput returns a file's contents.
type VFSReadOutput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Found   bool   `json:"found"`
}

// NewVFSReadTool creates the "vfs_read" tool.
func NewVFSReadTool(store VFSStore) (tool.InvokableTool, error) {
	return utils.InferTool("vfs_read",
		"Read the contents of a file from the virtual file system.",
		func(ctx context.Context, in VFSReadInput) (*VFSReadOutput, error) {
			chatID, err := vfsChatID(ctx)
			if err != nil {
				return nil, err
			}
			content, ok, err := store.VFSRead(ctx, chatID, in.Path)
			if err != nil {
				return nil, fmt.Errorf("tools: vfs_read: %w", err)
			}
			return &VFSReadOutput{Path: in.Path, Content: content, Found: ok}, nil
		})
}

// --- vfs_delete ---

// VFSDeleteInput is the model-facing schema for deleting a file.
type VFSDeleteInput struct {
	Path string `json:"path" jsonschema:"description=File path to delete"`
}

// VFSDeleteOutput confirms deletion.
type VFSDeleteOutput struct {
	Deleted bool   `json:"deleted"`
	Path    string `json:"path"`
}

// NewVFSDeleteTool creates the "vfs_delete" tool.
func NewVFSDeleteTool(store VFSStore) (tool.InvokableTool, error) {
	return utils.InferTool("vfs_delete",
		"Delete a file from the virtual file system.",
		func(ctx context.Context, in VFSDeleteInput) (*VFSDeleteOutput, error) {
			chatID, err := vfsChatID(ctx)
			if err != nil {
				return nil, err
			}
			if err := store.VFSDelete(ctx, chatID, in.Path); err != nil {
				return nil, fmt.Errorf("tools: vfs_delete: %w", err)
			}
			return &VFSDeleteOutput{Deleted: true, Path: in.Path}, nil
		})
}

// --- vfs_list ---

// VFSListOutput lists files in the virtual file system.
type VFSListOutput struct {
	Paths []string `json:"paths"`
}

// NewVFSListTool creates the "vfs_list" tool.
func NewVFSListTool(store VFSStore) (tool.InvokableTool, error) {
	return utils.InferTool("vfs_list",
		"List all file paths currently present in the virtual file system.",
		func(ctx context.Context, _ struct{}) (*VFSListOutput, error) {
			chatID, err := vfsChatID(ctx)
			if err != nil {
				return nil, err
			}
			paths, err := store.VFSList(ctx, chatID)
			if err != nil {
				return nil, fmt.Errorf("tools: vfs_list: %w", err)
			}
			sort.Strings(paths)
			return &VFSListOutput{Paths: paths}, nil
		})
}

// NewVFSTools returns the full VFS tool set for the ReAct loop.
func NewVFSTools(store VFSStore) ([]tool.BaseTool, error) {
	var out []tool.BaseTool
	for _, fn := range []func(VFSStore) (tool.InvokableTool, error){
		NewVFSWriteTool, NewVFSReadTool, NewVFSDeleteTool, NewVFSListTool,
	} {
		t, err := fn(store)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
