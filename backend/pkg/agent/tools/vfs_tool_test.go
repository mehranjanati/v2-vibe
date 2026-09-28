package tools

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

type fakeVFS struct {
	files map[string]string
}

func newFakeVFS() *fakeVFS { return &fakeVFS{files: map[string]string{}} }

func (f *fakeVFS) VFSWrite(_ context.Context, _, path, content string) error {
	f.files[path] = content
	return nil
}
func (f *fakeVFS) VFSRead(_ context.Context, _, path string) (string, bool, error) {
	v, ok := f.files[path]
	return v, ok, nil
}
func (f *fakeVFS) VFSDelete(_ context.Context, _, path string) error {
	delete(f.files, path)
	return nil
}
func (f *fakeVFS) VFSList(_ context.Context, _ string) ([]string, error) {
	out := make([]string, 0, len(f.files))
	for p := range f.files {
		out = append(out, p)
	}
	return out, nil
}

func TestVFSToolsRoundTrip(t *testing.T) {
	fs := newFakeVFS()
	tools, err := NewVFSTools(fs)
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("expected 4 vfs tools, got %d", len(tools))
	}
	byName := map[string]tool.InvokableTool{}
	for _, tl := range tools {
		inv, ok := tl.(tool.InvokableTool)
		if !ok {
			t.Fatalf("tool %T is not invokable", tl)
		}
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		byName[info.Name] = inv
	}
	ctx := WithVFSContext(context.Background(), "chat-1")

	if _, err := byName["vfs_write"].InvokableRun(ctx, `{"path":"src/index.html","content":"<h1>hi</h1>"}`); err != nil {
		t.Fatalf("vfs_write: %v", err)
	}
	if fs.files["src/index.html"] != "<h1>hi</h1>" {
		t.Fatalf("write not stored: %q", fs.files["src/index.html"])
	}

	res, err := byName["vfs_read"].InvokableRun(ctx, `{"path":"src/index.html"}`)
	if err != nil {
		t.Fatalf("vfs_read: %v", err)
	}
	if want := `{"path":"src/index.html","content":"<h1>hi</h1>","found":true}`; res != want {
		t.Fatalf("vfs_read output mismatch:\n got: %s\nwant: %s", res, want)
	}

	res, err = byName["vfs_list"].InvokableRun(ctx, `{}`)
	if err != nil {
		t.Fatalf("vfs_list: %v", err)
	}
	if res != `{"paths":["src/index.html"]}` {
		t.Fatalf("vfs_list unexpected: %s", res)
	}

	if _, err := byName["vfs_delete"].InvokableRun(ctx, `{"path":"src/index.html"}`); err != nil {
		t.Fatalf("vfs_delete: %v", err)
	}
	if len(fs.files) != 0 {
		t.Fatalf("delete did not remove file")
	}
}

func TestVFSNoContextFails(t *testing.T) {
	tools, err := NewVFSTools(newFakeVFS())
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	inv, ok := tools[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("tool is not invokable")
	}
	if _, err := inv.InvokableRun(context.Background(), `{"path":"x","content":"y"}`); err == nil {
		t.Fatal("expected error without chat context")
	}
}
