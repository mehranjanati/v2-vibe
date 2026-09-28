package engine

import (
	"context"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"backend/pkg/llm"
	"backend/pkg/models"
)

// assetRefRe matches local asset references inside generated HTML:
// <script src="..."> and <link rel="stylesheet" href="...">.
var (
	scriptSrcRe = regexp.MustCompile(`(?i)<script\b[^>]*\bsrc=["']([^"']+)["']`)
	linkHrefRe  = regexp.MustCompile(`(?i)<link\b[^>]*\bhref=["']([^"']+)["']`)
)

// maxGapFillRounds bounds the fill-missing-files passes per generation.
const maxGapFillRounds = 3

// modelsFileGenerating / modelsFileChunk / modelsFileGenerated are tiny
// constructors so this file does not repeat struct literals.
func modelsFileGenerating(path string) models.FileGenerating {
	return models.FileGenerating{Type: "file_generating", FilePath: path}
}

func modelsFileChunk(path, content string) models.FileChunkGenerated {
	return models.FileChunkGenerated{Type: "file_chunk_generated", FilePath: path, Chunk: content}
}

func modelsFileGenerated(path, content string) models.FileGenerated {
	return models.FileGenerated{
		Type: "file_generated",
		File: &models.FileEntry{FilePath: path, FileContents: content},
	}
}

// applyFileEvents routes stream-parser events to the VFS and the WS
// broadcasts (file_generating / file_chunk_generated / file_generated),
// bumping filesWritten for every completed file.
func (r *ProjectRoom) applyFileEvents(evs []llm.StreamEvent) {
	for _, ev := range evs {
		switch ev.Kind {
		case llm.EventStart:
			debugLogEvent(r, "file_generating", "seq", debugNextSeq(), "path", ev.Path)
			r.BroadcastMessage(modelsFileGenerating(ev.Path))
		case llm.EventChunk:
			debugLogEvent(r, "file_chunk_generated", "path", ev.Path, "bytes", len(ev.Content))
			r.BroadcastMessage(modelsFileChunk(ev.Path, ev.Content))
		case llm.EventEnd:
			debugLogEvent(r, "file_generated", "path", ev.Path, "bytes", len(ev.Content))
			content := ev.Content
			if llm.LooksLikeJS(ev.Path) {
				// Repair LLM syntax slips (empty values, truncated
				// decimals) BEFORE the file reaches the VFS/preview.
				content = llm.SanitizeJS(content)
			}
			r.UpsertFile(ev.Path, content)
			r.BroadcastMessage(modelsFileGenerated(ev.Path, content))
			r.filesWritten.Add(1)
		}
	}
}

// streamAndApplyFiles runs ONE raw streaming pass whose output is parsed
// into per-file events and applied to the VFS/WS. Returns the full raw
// output (for fence fallback parsing), the salvage-flagged (truncated)
// paths, and any stream error.
func (r *ProjectRoom) streamAndApplyFiles(
	ctx context.Context,
	system string,
	msgs []llm.ChatMessage,
	maxTokens int,
) (string, []string, error) {
	parser := llm.NewStreamParser()
	output, _, err := r.streamLLMRaw(ctx, system, msgs, maxTokens, func(delta string) {
		r.applyFileEvents(parser.Feed(delta))
	})
	if err != nil {
		return output, nil, err
	}
	r.applyFileEvents(parser.Flush())
	return output, parser.TruncatedPaths(), nil
}

// missingReferencedAssets scans the entry index.html for locally
// referenced scripts/stylesheets that do NOT exist in the VFS. These are
// files the stream was cut BEFORE the model ever opened (they are not
// salvage-flagged — no block was started) and they silently break the
// preview (e.g. a missing main.js means the app never boots).
func missingReferencedAssets(files map[string]*models.FileEntry) []string {
	if len(files) == 0 {
		return nil
	}
	norm := func(p string) string { return strings.TrimPrefix(p, "/") }

	byNorm := make(map[string]*models.FileEntry, len(files))
	for p, e := range files {
		byNorm[norm(p)] = e
	}

	// Locate the entry HTML.
	entry := ""
	for _, cand := range []string{"public/index.html", "index.html", "src/index.html"} {
		if byNorm[cand] != nil {
			entry = cand
			break
		}
	}
	if entry == "" {
		return nil
	}
	html := byNorm[entry].FileContents
	dir := path.Dir(entry)
	if dir == "." {
		dir = ""
	}

	var refs []string
	seenRef := map[string]bool{}
	for _, re := range []*regexp.Regexp{scriptSrcRe, linkHrefRe} {
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) > 1 && !seenRef[m[1]] {
				seenRef[m[1]] = true
				refs = append(refs, m[1])
			}
		}
	}

	var missing []string
	for _, ref := range refs {
		clean := strings.TrimSpace(ref)
		lower := strings.ToLower(clean)
		if clean == "" || !strings.Contains(clean, ".") ||
			strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
			strings.HasPrefix(clean, "//") || strings.HasPrefix(clean, "data:") ||
			strings.HasSuffix(lower, ".json") ||
			(!strings.HasSuffix(lower, ".js") && !strings.HasSuffix(lower, ".css")) {
			continue
		}
		candidates := []string{clean, "/" + clean}
		resolved := clean
		if dir != "" {
			candidates = append(candidates, dir+"/"+clean, "/"+dir+"/"+clean)
			// Report the path RELATIVE TO THE ENTRY DIR (e.g.
			// "public/js/main.js") so gap-fill writes into the same tree
			// as the rest of the app — a stray root-level "js/main.js"
			// would be dropped by the static preview bundler.
			resolved = dir + "/" + clean
		}
		found := false
		for _, c := range candidates {
			if byNorm[norm(c)] != nil {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, resolved)
		}
	}
	sort.Strings(missing)
	return missing
}

// writeSafeStubForMissing writes a minimal, syntax-safe stub for a
// still-missing asset so the preview never references a file that does not
// exist. .js files get an App-namespace script (deriving element ids/classes
// from the entry index.html so the stub at least does something visible);
// .css files get a minimal valid stylesheet (an empty/missing stylesheet is
// valid CSS, so the preview renders un-styled instead of 404-ing).
func (r *ProjectRoom) writeSafeStubForMissing(ctx context.Context, path string) {
	if ctx.Err() != nil {
		return
	}
	if strings.HasSuffix(path, ".css") {
		r.writeSafeStubForMissingCSS(path)
		return
	}
	// Extract ids/classes from index.html for a small amount of intent.
	entry := entryHTMLPath(r.GetVFS())
	html := ""
	if entry != "" {
		if e, ok := r.GetVFS()[entry]; ok {
			html = e.FileContents
		}
	}
	if ctx.Err() != nil {
		return
	}
	idRE := regexp.MustCompile(`id="([A-Za-z_][\w-]*)"`)
	classRE := regexp.MustCompile(`class="([A-Za-z_][\w-\s]*)"`)
	var ids []string
	seen := map[string]bool{}
	var initLines []string
	for _, m := range idRE.FindAllStringSubmatch(html, -1) {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
			// Try to make the stub useful: populate containers with a
			// simple "loaded" message so the preview shows content rather
			// than staying blank.
			initLines = append(initLines,
				fmt.Sprintf("  var el = document.getElementById(%q); if (el) { el.innerHTML = '<div class=\"stub-note\">Section loaded.</div>'; }", id))
		}
	}
	if len(initLines) == 0 {
		for _, m := range classRE.FindAllStringSubmatch(html, -1) {
			cls := m[1]
			if !seen["cb:"+cls] && !strings.Contains(cls, " ") {
				seen["cb:"+cls] = true
				initLines = append(initLines,
					fmt.Sprintf("  var el = document.querySelector(%q); if (el) { el.innerHTML = '<div class=\"stub-note\">%s loaded.</div>'; }", "."+cls, cls))
			}
		}
	}
	stub := "window.App = window.App || {};\n"
	stub += "/** Auto-generated safe stub (gap-fill could not produce this file). */\n"
	// Provide a no-op for the module the file would define (best-effort).
	mod := strings.TrimSuffix(filepath.Base(path), ".js")
	if mod != "" && mod != "index" && mod != "main" {
		stub += "App." + mod + " = App." + mod + " || {};\n"
	}
	if len(initLines) > 0 {
		stub += "document.addEventListener('DOMContentLoaded', function () {\n"
		stub += strings.Join(initLines, "\n") + "\n"
		stub += "});\n"
	}
	r.UpsertFile(path, stub)
	r.BroadcastMessage(modelsFileGenerated(path, stub))
	r.filesWritten.Add(1)
	log.Printf("[room:%s] wrote safe fallback stub for missing %s", r.chatID, path)
}

// standardAppFiles is the fallback manifest used when the entry index.html
// was never generated (stream cut before the model emitted it): missing-file
// discovery by HTML scan is then impossible, so the well-known contract
// files from generationSystemPrompt fill in instead.
var standardAppFiles = []string{
	"public/index.html",
	"public/styles.css",
	"public/js/data.js",
	"public/js/store.js",
	"public/js/products.js",
	"public/js/cart.js",
	"public/js/main.js",
}

// missingOrContractFiles returns the files missing from the VFS: assets
// referenced by the entry index.html when one exists, otherwise (the stream
// died before index.html was emitted) the standard file contract.
func (r *ProjectRoom) missingOrContractFiles() []string {
	missing := missingReferencedAssets(r.GetVFS())
	if entryHTMLPath(r.GetVFS()) != "" {
		return missing
	}
	// HTML-based asset discovery is impossible — fall back to the
	// standard file contract so the remaining files still get generated.
	have := map[string]bool{}
	for p := range r.GetVFS() {
		have[strings.TrimPrefix(p, "/")] = true
	}
	for _, p := range standardAppFiles {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	return missing
}

// fillMissingReferencedFiles runs up to maxGapFillRounds gap-fill passes:
// detect entry-HTML-referenced files missing from the VFS and stream them
// in with dedicated, context-rich requests. If the model still cannot
// produce a file (connection drops, truncation, refusal), writeSafeStub
// for the remainder so the preview never references a missing asset.
//
// The prompt embeds the app's REAL context (entry index.html + existing
// JS) so the model writes integrating, functional files instead of the
// generic broken stubs it produces without context (empty `id: ""`,
// `price: .`, wrong element ids).
func (r *ProjectRoom) fillMissingReferencedFiles(ctx context.Context) {
	for round := 1; round <= maxGapFillRounds; round++ {
		missing := r.missingOrContractFiles()
		if len(missing) == 0 {
			return
		}
		log.Printf("[room:%s] gap-fill round %d: %d referenced files missing: %s",
			r.chatID, round, len(missing), strings.Join(missing, ", "))

		gapSystem := "You are a web app generator completing a PARTIALLY GENERATED app. " +
			"The app's index.html and some JS files already exist and are PROVIDED BELOW — " +
			"your new files MUST integrate with them.\n" +
			"INTEGRATION RULES (critical):\n" +
			"- Read index.html and render into the REAL element ids/classes it defines " +
			"(e.g. if it has <div class=\"product-grid\">, render there — never invent new containers).\n" +
			"- Reuse the existing global namespace: if data.js defines App.data, render FROM App.data — " +
			"never invent placeholder products or overwrite existing App.* members.\n" +
			"- Plain browser JS attached to the shared App object. No imports, no modules, no React.\n" +
			"SYNTAX RULES (critical — one violation blanks the whole app):\n" +
			"- NEVER write empty values: `id: ,` / `price: .` / `id: \"\"` are FORBIDDEN. " +
			"Write real values (e.g. `id: \"p1\"`, `price: 4.50`).\n" +
			"- Every string closed, every brace/bracket matched, no truncated numbers.\n" +
			"QUALITY RULES: implement REAL functionality (rendering, filtering, cart actions) — " +
			"never console.log-only stubs. Match the styling classes already used in index.html.\n" +
			"OUTPUT: only fenced code blocks with the file path as the info string, FULL file content."

		var ctxBuilder strings.Builder
		if entry := entryHTMLPath(r.GetVFS()); entry != "" {
			if e := r.GetVFS()[entry]; e != nil {
				ctxBuilder.WriteString("\n--- EXISTING index.html (render into THESE ids/classes) ---\n")
				ctxBuilder.WriteString(clip(e.FileContents, 8000))
				ctxBuilder.WriteString("\n")
			}
		}
		for _, p := range sortedJSFiles(r.GetVFS()) {
			if e := r.GetVFS()[p]; e != nil {
				ctxBuilder.WriteString("\n--- EXISTING " + p + " (reuse its App.* API; do not redefine) ---\n")
				ctxBuilder.WriteString(clip(e.FileContents, 5000))
				ctxBuilder.WriteString("\n")
			}
		}

		gapMsg := "Missing files to generate:\n" +
			strings.Join(missing, "\n") +
			"\n" + ctxBuilder.String() +
			"\nGenerate each missing file completely now, as fenced code blocks with the exact paths above."

		filesBefore := r.filesWritten.Load()
		out, truncated, err := r.streamAndApplyFiles(ctx, gapSystem,
			[]llm.ChatMessage{{Role: "user", Content: gapMsg}}, 24576)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[room:%s] gap-fill pass failed: %v", r.chatID, err)
			// Hard LLM failure: stop retrying but still fall through to
			// the stub pass below so the preview never references a
			// missing asset.
			break
		}
		if r.filesWritten.Load() == filesBefore {
			// Zero files parsed from this pass: log the raw output head so
			// the failure mode (prose reply, wrong fence info strings,
			// refusal, ...) is visible in the logs.
			log.Printf("[room:%s] gap-fill round %d parsed 0 files (raw %d bytes): %.200q",
				r.chatID, round, len(out), out)
		}
		if len(truncated) > 0 {
			log.Printf("[room:%s] gap-fill pass truncated %d files; another round will retry",
				r.chatID, len(truncated))
		}
	}

	// Fallback: any files STILL missing after all rounds get a safe stub
	// so the preview never references a file that does not exist.
	// missingOrContractFiles (not the HTML scan alone): when index.html
	// itself is missing, the scan finds nothing and stubs would be skipped.
	// Both .js and .css get stubs: a missing stylesheet otherwise leaves the
	// preview unstyled (A4), exactly like a missing script blanks it.
	still := r.missingOrContractFiles()
	for _, p := range still {
		if !strings.HasSuffix(p, ".js") && !strings.HasSuffix(p, ".css") {
			continue
		}
		// p is already entry-dir-relative (e.g. "public/js/main.js").
		r.writeSafeStubForMissing(ctx, p)
	}
	if n := len(still); n > 0 {
		log.Printf("[room:%s] gap-fill exhausted; wrote safe stubs for remaining %d missing assets",
			r.chatID, n)
	}
}

// writeSafeStubForMissingCSS writes a minimal valid stylesheet for a
// still-missing .css asset (A4). An empty stylesheet is valid CSS, so the
// preview renders without 404 styling loss instead of the link failing.
func (r *ProjectRoom) writeSafeStubForMissingCSS(path string) {
	stub := "/* Auto-generated safe stub (gap-fill could not produce this file). */\n"
	r.UpsertFile(path, stub)
	r.BroadcastMessage(modelsFileGenerated(path, stub))
	r.filesWritten.Add(1)
	log.Printf("[room:%s] wrote safe CSS stub for missing %s", r.chatID, path)
}
func entryHTMLPath(files map[string]*models.FileEntry) string {
	for _, cand := range []string{"public/index.html", "index.html", "src/index.html"} {
		for p := range files {
			if strings.TrimPrefix(p, "/") == cand {
				return cand
			}
		}
	}
	return ""
}

// sortedJSFiles lists existing .js files (entry dir first), smallest
// footprint order for prompt context.
func sortedJSFiles(files map[string]*models.FileEntry) []string {
	var out []string
	for p := range files {
		if strings.HasSuffix(p, ".js") {
			out = append(out, strings.TrimPrefix(p, "/"))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return len(files[out[i]].FileContents) < len(files[out[j]].FileContents)
	})
	return out
}

// clip truncates s to n bytes on a line boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut + "\n…(truncated)"
}
