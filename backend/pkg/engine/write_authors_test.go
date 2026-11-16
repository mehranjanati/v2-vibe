package engine

import (
	"context"
	"testing"
)

// TestGenerationFileAuthorsPerWritePath is the P1.3.3 acceptance test for
// write attribution: every write path the engine has today lands its explicit
// author in `generation_files.author_agent` after one real Finish, and an
// unattributed (manual) write during the run sets `generations.fork`.
//
// The paths exercised are the production ones, not stand-ins:
//   - roomVFSStore.VFSWrite / VFSDelete — the vfs_write/vfs_delete tool path
//     shared by plan-execute and the team (author "coder");
//   - writeSafeStubForMissingCSS — the gap-fill recovery (author "gapfill");
//   - UpsertFile(..., "") — a manual/editor/import-style write with no agent
//     identity (NULL author, fork=1).
func TestGenerationFileAuthorsPerWritePath(t *testing.T) {
	f, d1 := newFakeD1(t)
	room := NewProjectRoom(lineageChatID, nil, nil, nil)
	room.genKV = newFakeGenerationKV()
	room.SetD1Client(d1)

	// Baseline BEFORE the run: the delete path needs a file that already
	// existed, and its row must keep the author of the actor that deleted it.
	room.UpsertFile("public/js/replaced.js", "// v1", writeAuthorCoder)

	genID, err := room.StartGenerationRecord(context.Background(), "")
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}

	store := roomVFSStore{r: room, author: writeAuthorCoder}
	if err := store.VFSWrite(context.Background(), room.chatID, "public/js/app.js", "console.log(1);"); err != nil {
		t.Fatalf("tool write: %v", err)
	}
	if err := store.VFSDelete(context.Background(), room.chatID, "public/js/replaced.js"); err != nil {
		t.Fatalf("tool delete: %v", err)
	}
	room.writeSafeStubForMissingCSS("public/css/app.css")
	room.UpsertFile("public/import/data.json", "{}", "")

	if err := room.FinishGenerationRecord(context.Background(), genID, "done"); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	gen := f.generation(t, genID)
	if gen.status != "succeeded" {
		t.Errorf("generation status = %q, want succeeded", gen.status)
	}
	if !gen.fork {
		t.Fatal("a manual write during the run must set generations.fork")
	}

	want := map[string]string{
		"public/js/app.js":        writeAuthorCoder,
		"public/css/app.css":      writeAuthorGapfill,
		"public/js/replaced.js":   writeAuthorCoder, // delete row: author = who deleted
		"public/import/data.json": "",               // manual write: NULL author
	}
	rows := f.fileRows()
	if len(rows) != len(want) {
		t.Fatalf("file rows = %d (%+v), want %d", len(rows), rows, len(want))
	}
	ops := make(map[string]string, len(rows))
	for _, row := range rows {
		author, known := want[row.path]
		if !known {
			t.Fatalf("unexpected file row %+v", row)
		}
		if row.author != author {
			t.Errorf("%s author = %q, want %q", row.path, row.author, author)
		}
		ops[row.path] = row.op
	}
	if ops["public/js/app.js"] != "create" || ops["public/css/app.css"] != "create" {
		t.Errorf("ops = %v, want app.js/app.css create", ops)
	}
	if ops["public/js/replaced.js"] != "delete" {
		t.Errorf("replaced.js op = %q, want delete", ops["public/js/replaced.js"])
	}

	// Audit trail: one file_written row per attributed file, plus the fork
	// marker (the manual write has no author, so it has no file_written row).
	audits := f.waitForAudits(t, 5)
	actions := make(map[string]int)
	for _, a := range audits {
		actions[a.action]++
		if a.action == "file_written" && a.actor == "user" {
			t.Errorf("manual write must not be attributed to an actor: %+v", a)
		}
	}
	if actions["generation_finished"] != 1 || actions["generation_forked"] != 1 || actions["file_written"] != 3 {
		t.Errorf("audit actions = %v, want finished=1 forked=1 file_written=3", actions)
	}
}

// TestGenerationForkFlagUnsetForAgentOnlyRun pins the negative: a run where
// only agent paths wrote keeps fork=false and records no fork audit, so the
// flag really means "a human diverged", not "a run happened".
func TestGenerationForkFlagUnsetForAgentOnlyRun(t *testing.T) {
	f, d1 := newFakeD1(t)
	room := NewProjectRoom(lineageChatID, nil, nil, nil)
	room.genKV = newFakeGenerationKV()
	room.SetD1Client(d1)

	genID, err := room.StartGenerationRecord(context.Background(), "")
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	room.UpsertFile("public/js/app.js", "console.log(1);", writeAuthorCoder)
	if err := room.FinishGenerationRecord(context.Background(), genID, "done"); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	if gen := f.generation(t, genID); gen.fork {
		t.Fatal("agent-only run must not set generations.fork")
	}
	for _, a := range f.waitForAudits(t, 2) {
		if a.action == "generation_forked" {
			t.Fatalf("agent-only run recorded a fork audit: %+v", a)
		}
	}
}
