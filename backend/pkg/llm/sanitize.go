package llm

import (
	"regexp"
	"strings"
)

// LLM-generated JS sometimes contains empty values and truncated decimals
// that are hard syntax errors (`key: ,` / `price: . }`) which blank the
// whole preview. SanitizeJS repairs the well-known failure modes — the
// same rules the frontend preview applies, so the VFS already contains
// parseable code.
var (
	sanEmptyIdent  = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*:\s*,`)
	sanEmptyQuoted = regexp.MustCompile(`(["'][^"']*["'])\s*:\s*,`)
	sanEmptyDot    = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*:\s*\.(\s*[,}\]])`)
	sanTruncDec    = regexp.MustCompile(`(\d+)\.(\s*[,}\]])`)
	// Comparison against a dropped literal: `minPrice !== &&` — the model
	// emitted the literal (usually "" ) and it vanished. Repair to "".
	sanCmpEmpty = regexp.MustCompile(`(!==|===|!=|==)\s*(&&|\|\||\)|;|,)`)
	// Dropped operand after ||: `parseFloat(x) ||;` → `|| 0;`.
	sanOrSemi  = regexp.MustCompile(`\|\|\s*;`)
	sanOrComma = regexp.MustCompile(`\|\|\s*,`)
)

// SanitizeJS repairs common LLM-induced JavaScript syntax errors:
//   - empty value after a key:        `rating: ,`   -> `rating: null,`
//   - empty string-keyed value:       `'x': ,`      -> `'x': null,`
//   - dot with no digits:             `price: .`    -> `price: null`
//   - truncated decimal:              `rating: 4.`  -> `rating: 4`
//   - empty comparison operand:       `x !== &&`    -> `x !== "" &&`
//   - empty || operand before `;`/`,`: `f() ||;`    -> `f() || 0;`
func SanitizeJS(code string) string {
	out := sanEmptyIdent.ReplaceAllString(code, "$1: null,")
	out = sanEmptyQuoted.ReplaceAllString(out, "$1: null,")
	out = sanEmptyDot.ReplaceAllString(out, "$1: null$2")
	out = sanTruncDec.ReplaceAllString(out, "$1$2")
	out = sanCmpEmpty.ReplaceAllString(out, `$1 "" $2`)
	out = sanOrSemi.ReplaceAllString(out, "|| 0;")
	out = sanOrComma.ReplaceAllString(out, "|| 0,")
	return out
}

// LooksLikeJS reports whether path is a JavaScript file.
func LooksLikeJS(path string) bool {
	return strings.HasSuffix(path, ".js")
}
