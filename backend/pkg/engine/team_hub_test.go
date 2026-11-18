package engine

import (
	"strings"
	"testing"
)

func TestHubFilesRanksReferencedFiles(t *testing.T) {
	files := map[string]string{
		"public/index.html": `<script src="src/store.js"></script><script src="src/app.js"></script>`,
		"src/app.js":        `import { store } from './store.js';`,
		"src/store.js":      `export const store = {};`,
		"src/about.js":      `document.title = 'about';`,
	}
	plan := "Build a todo app whose state lives in src/store.js and renders from src/app.js"

	hubs := HubFiles(plan, files)
	// store.js: referenced by index.html + app.js + the plan (3) → first.
	// app.js:   referenced by index.html + the plan (2)      → second.
	// about.js: referenced by nobody                          → absent.
	if len(hubs) != 2 || hubs[0] != "src/store.js" || hubs[1] != "src/app.js" {
		t.Fatalf("HubFiles = %v, want [src/store.js src/app.js] in that order", hubs)
	}
	for _, hub := range hubs {
		if hub == "src/about.js" {
			t.Errorf("a leaf file must never be a hub: %v", hubs)
		}
	}
}

func TestHubFilesNeedsTwoInboundReferences(t *testing.T) {
	// Referenced by one sibling and not by the plan: not a hub.
	files := map[string]string{
		"index.html": `<link rel="stylesheet" href="styles.css">`,
		"styles.css": `body { color: red }`,
		"app.js":     `console.log(1)`,
	}
	if hubs := HubFiles("build a page", files); len(hubs) != 0 {
		t.Fatalf("HubFiles = %v, want none (single inbound reference)", hubs)
	}
}

func TestHubFilesCapsAndOrdersDeterministically(t *testing.T) {
	// Four candidates, all referenced by every other file; the cap keeps three
	// and the tie-break is the path, so repeated runs give the same list.
	files := map[string]string{
		"a.css": "x",
		"b.css": "x",
		"c.css": "x",
		"d.css": "x",
	}
	for _, path := range []string{"a.css", "b.css", "c.css", "d.css"} {
		var b strings.Builder
		b.WriteString("references: ")
		for _, other := range []string{"a.css", "b.css", "c.css", "d.css"} {
			b.WriteString(other)
			b.WriteString(" ")
		}
		files[path] = b.String()
	}
	hubs := HubFiles("", files)
	if len(hubs) != reviewerHubLimit {
		t.Fatalf("HubFiles returned %d entries, want the cap %d", len(hubs), reviewerHubLimit)
	}
	if hubs[0] != "a.css" || hubs[1] != "b.css" || hubs[2] != "c.css" {
		t.Fatalf("HubFiles = %v, want a.css,b.css,c.css by path tie-break", hubs)
	}
}

func TestHubFilesEmptyVFS(t *testing.T) {
	if hubs := HubFiles("anything", nil); hubs != nil {
		t.Fatalf("HubFiles(nil) = %v, want nil", hubs)
	}
}

func TestGenerationChangedFilesDiffsTheBaseline(t *testing.T) {
	room := NewProjectRoom("p16-diff", nil, nil, nil)
	room.storeGenerationSnapshot("gen-p16", &generationSnapshot{
		ChatID: room.chatID,
		Files: map[string]snapshotFile{
			"index.html": hashVFSFile("<h1>one</h1>"),
			"old.css":    hashVFSFile("body{}"),
		},
	})

	room.UpsertFile("index.html", "<h1>two</h1>", writeAuthorCoder) // modify
	room.UpsertFile("app.js", "console.log(1)", writeAuthorCoder)   // create
	// old.css is absent from the VFS now → delete

	changed := room.generationChangedFiles()
	want := []changedFile{
		{Path: "app.js", Op: "create"},
		{Path: "index.html", Op: "modify"},
		{Path: "old.css", Op: "delete"},
	}
	if len(changed) != len(want) {
		t.Fatalf("generationChangedFiles = %#v, want %#v", changed, want)
	}
	for i := range want {
		if changed[i] != want[i] {
			t.Errorf("row %d = %#v, want %#v", i, changed[i], want[i])
		}
	}
}

func TestGenerationChangedFilesWithoutBaseline(t *testing.T) {
	room := NewProjectRoom("p16-nobase", nil, nil, nil)
	room.UpsertFile("index.html", "<h1>x</h1>", writeAuthorCoder)
	if got := room.generationChangedFiles(); got != nil {
		t.Fatalf("changed files without a baseline = %#v, want nil", got)
	}
}

func TestReviewerInstructionCarriesDiffAndHubs(t *testing.T) {
	instruction := reviewerInstruction(
		"BASE REVIEWER SKILL",
		[]changedFile{{Path: "src/store.js", Op: "modify"}, {Path: "old.css", Op: "delete"}},
		[]string{"src/store.js", "styles.css"},
	)

	for _, want := range []string{
		"BASE REVIEWER SKILL",
		"- modify src/store.js",
		"- delete old.css",
		"HUB: src/store.js, styles.css",
		"RELATIVE",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("instruction is missing %q:\n%s", want, instruction)
		}
	}
	// Path + op only: no file contents are ever pasted in.
	if strings.Contains(instruction, "console.log") {
		t.Error("the instruction leaked file contents")
	}

	empty := reviewerInstruction("SKILL", nil, nil)
	if !strings.Contains(empty, "No files have changed yet") {
		t.Errorf("empty diff wording missing:\n%s", empty)
	}
	if strings.Contains(empty, "HUB:") {
		t.Error("no hub line should be printed when there are no hubs")
	}
}
