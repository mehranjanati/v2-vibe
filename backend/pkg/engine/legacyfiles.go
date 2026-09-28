package engine

// legacyFiles.go — single source of truth for legacy pseudo-file paths.
//
// B10/C3: early builds persisted the raw LLM transcript into the VFS as a
// pseudo-file ("generated-output.txt"). StreamParser + applyEvents no longer
// do this, but old Redis hashes still carry the key. Both VFS readers
// (hub.GetVFSReadOnly and room.loadVFS) must agree on which paths to hide /
// delete, so the check lives here instead of being duplicated inline.
//
// Keep the frontend's IGNORED_FILES list (src/components/preview/
// preview-normalize.ts) in sync: it currently lists both spellings too.

// legacyOutputFileBase is the transcript pseudo-file name as early builds
// wrote it.
const legacyOutputFileBase = "generated-output.txt"

// IsLegacyOutputFile reports whether path is the raw LLM transcript
// pseudo-file (with or without the leading slash). Such paths must never be
// surfaced to clients and should be deleted from persisted VFS hashes.
func IsLegacyOutputFile(path string) bool {
	return path == legacyOutputFileBase || path == "/"+legacyOutputFileBase
}
