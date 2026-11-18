package engine

import (
	"sort"
	"strings"
)

// ---------- P1.6: diff-aware reviewer ----------
//
// The reviewer used to judge each file in isolation ("is this file clean?"),
// which cannot see the two failure modes that actually break a generated app: a
// change that contradicts what the previous generation had, and a change to a
// file everything else depends on. Both are cheap to compute from data the room
// already holds, so they travel with the reviewer's instruction instead of
// relying on the coordinator to mention them.
//
// Naming: hub FILES are the most-referenced files of one app. `hub.go` is taken
// by EngineHub (the room registry) and the two must not be confused, hence this
// separate file.

const (
	// reviewerHubLimit caps how many hub files are named in the reviewer task.
	reviewerHubLimit = 3
	// reviewerHubMinRefs is the inbound-reference count that makes a file a hub.
	reviewerHubMinRefs = 2
	// reviewerChangedLimit caps the changed-file list. Path + op only, so even a
	// large plan keeps the reviewer context small.
	reviewerChangedLimit = 40
)

// changedFile is one path the current generation touched.
type changedFile struct {
	Path string
	Op   string
}

// generationChangedFiles diffs the open generation's baseline against the VFS as
// it stands now — the same rows FinishGenerationRecord will write later, but
// available while the team is still running.
func (r *ProjectRoom) generationChangedFiles() []changedFile {
	r.lineageMu.Lock()
	baseline := r.lineageSnapshot
	r.lineageMu.Unlock()
	if baseline == nil {
		return nil
	}
	rows := diffGenerationFiles(baseline.Files, r.currentVFSHashes(), nil)
	out := make([]changedFile, 0, len(rows))
	for _, row := range rows {
		out = append(out, changedFile{Path: row.Path, Op: row.Op})
		if len(out) >= reviewerChangedLimit {
			break
		}
	}
	return out
}

// HubFiles returns the files a change would ripple through, most-referenced
// first: a file counts when its base name appears in the CONTENT of at least
// reviewerHubMinRefs other files, or in the plan text. So `store.js` and a
// shared `styles.css` surface while leaf pages do not. Deterministic (score
// desc, then path asc) so the reviewer instruction is stable across runs.
func HubFiles(planText string, files map[string]string) []string {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	plan := strings.ToLower(planText)
	counts := make(map[string]int, len(paths))
	for _, target := range paths {
		base := strings.ToLower(baseName(target))
		if base == "" {
			continue
		}
		count := 0
		for _, source := range paths {
			if source == target {
				continue
			}
			if strings.Contains(strings.ToLower(files[source]), base) {
				count++
			}
		}
		if plan != "" && strings.Contains(plan, base) {
			count++
		}
		counts[target] = count
	}

	type scored struct {
		path  string
		score int
	}
	ranked := make([]scored, 0, len(paths))
	for _, path := range paths {
		if counts[path] >= reviewerHubMinRefs {
			ranked = append(ranked, scored{path: path, score: counts[path]})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].path < ranked[j].path
	})

	out := make([]string, 0, reviewerHubLimit)
	for _, item := range ranked {
		out = append(out, item.path)
		if len(out) >= reviewerHubLimit {
			break
		}
	}
	return out
}

// reviewerInstruction appends this run's diff and hub files to the reviewer's
// skill prompt. Path + op only: the reviewer reads contents itself, and pasting
// contents here would undo the context budget that per-file review relies on.
func reviewerInstruction(skillPrompt string, changed []changedFile, hubs []string) string {
	var b strings.Builder
	b.WriteString(skillPrompt)
	b.WriteString("\n\n## This run's diff\n\n")
	if len(changed) == 0 {
		b.WriteString("No files have changed yet. If that is unexpected for this task, say so in the verdict.\n")
	} else {
		b.WriteString("Files this run changed (path + op only — read them yourself):\n")
		for _, file := range changed {
			b.WriteString("- ")
			b.WriteString(file.Op)
			b.WriteString(" ")
			b.WriteString(file.Path)
			b.WriteString("\n")
		}
	}
	if len(hubs) > 0 {
		b.WriteString("\nHUB: ")
		b.WriteString(strings.Join(hubs, ", "))
		b.WriteString(" — changes here have a high blast radius; check every inbound reference (script/link src, selectors, token names) before approving.\n")
	}
	b.WriteString("\nJudge this RELATIVE to the previous generation: is the change correct given what the app already had, not merely whether this file is tidy.\n")
	return b.String()
}

// baseName returns the last path segment.
func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
