package engine

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// argStrings flattens the string elements of an FT.* argument list.
func argStrings(args []interface{}) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// containsSequence reports whether args contains seq consecutively.
func containsSequence(args []interface{}, seq ...string) bool {
	if len(seq) == 0 || len(args) < len(seq) {
		return false
	}
	for i := 0; i+len(seq) <= len(args); i++ {
		ok := true
		for j, want := range seq {
			got, isStr := args[i+j].(string)
			if !isStr || got != want {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// TestBuildSearchArgs pins the exact FT.SEARCH command the RAG path sends.
//
// Every one of these details was wrong before and each failure is silent:
// without DIALECT 2 the `=>[KNN …]` syntax is rejected outright
// ("Syntax error at offset 1 near >["), sorting by the non-existent
// `__embedding_score` alias fails, RETURNing `path`/`text` returns nothing
// (they live inside the `metadata` JSON blob), and an unscoped `*` query
// leaks other projects' files into the planner context.
func TestBuildSearchArgs(t *testing.T) {
	const chatID = "32c839d0-2f12-49d9-bb89-22bd1cb89813"
	blob := Float32ToBytes(HashEmbedding("build simple coffee landing"))
	args := buildSearchArgs(chatID, blob, 6)
	joined := strings.Join(argStrings(args), " ")

	// 1. DIALECT 2 is mandatory for the vector-query syntax.
	if !containsSequence(args, "DIALECT", "2") {
		t.Errorf("FT.SEARCH must pass DIALECT 2, got %v", argStrings(args))
	}
	// 2. The distance alias is created in the query and used for sorting.
	if !strings.Contains(joined, "$BLOB AS score") {
		t.Errorf("query must alias the distance as $BLOB AS score: %s", joined)
	}
	if !containsSequence(args, "SORTBY", "score", "ASC") {
		t.Errorf("SORTBY score ASC missing (COSINE distance: smaller = closer): %v", argStrings(args))
	}
	if strings.Contains(joined, "__embedding_score") {
		t.Errorf("the implicit __embedding_score alias does not exist: %s", joined)
	}
	// 3. RETURN names real hash fields.
	if !containsSequence(args, "RETURN", "2", "metadata", "score") {
		t.Errorf("RETURN must request metadata + score: %v", argStrings(args))
	}
	// 4. The search is scoped to this room.
	if !strings.Contains(joined, "@chat:{") {
		t.Errorf("search must be scoped by the chat tag: %s", joined)
	}
	if !strings.Contains(joined, "KNN 6 @embedding") {
		t.Errorf("KNN k=6 missing: %s", joined)
	}
	if args[0] != "FT.SEARCH" || args[1] != vectorIndexName {
		t.Errorf("command/index = %v %v", args[0], args[1])
	}
	if !containsSequence(args, "PARAMS", "2", "BLOB") {
		t.Errorf("PARAMS count must be 2 (one BLOB pair): %v", argStrings(args))
	}
	// The blob is passed as a raw []byte parameter, not a string.
	foundBlob := false
	for i, a := range args {
		if s, ok := a.(string); ok && s == "BLOB" && i+1 < len(args) {
			raw, isBytes := args[i+1].([]byte)
			if !isBytes || len(raw) != vectorDim*4 {
				t.Errorf("BLOB param = %T len %d, want []byte len %d",
					args[i+1], len(raw), vectorDim*4)
			}
			foundBlob = true
		}
	}
	if !foundBlob {
		t.Errorf("no BLOB param in %v", argStrings(args))
	}
}

// TestBuildSearchArgsEscapesChatID: chat IDs are UUIDs but can also be
// caller-supplied slugs (e.g. "carstore-1788777330553"), whose `-` is a
// TAG-query metacharacter. Unescaped it silently narrows/changes the match.
func TestBuildSearchArgsEscapesChatID(t *testing.T) {
	blob := Float32ToBytes(HashEmbedding("query"))
	cases := []struct {
		chatID string
		want   string
	}{
		{"abc123", "@chat:{abc123}=>[KNN 3 @embedding $BLOB AS score]"},
		{"carstore-1788777330553", `@chat:{carstore\-1788777330553}=>[KNN 3 @embedding $BLOB AS score]`},
		{"we@ird:id", `@chat:{we\@ird\:id}=>[KNN 3 @embedding $BLOB AS score]`},
	}
	for _, tc := range cases {
		args := buildSearchArgs(tc.chatID, blob, 3)
		filter, _ := args[2].(string)
		if filter != tc.want {
			t.Errorf("chatID %q filter = %q, want %q", tc.chatID, filter, tc.want)
		}
	}
}

// TestBuildCreateArgsHasChatTag: the index schema must carry the `chat`
// TAG field the scoped query filters on, else FT.SEARCH errors out with
// "Property `chat` not loaded from schema".
func TestBuildCreateArgsHasChatTag(t *testing.T) {
	args := buildCreateArgs()
	if !containsSequence(args, "SCHEMA") {
		t.Errorf("FT.CREATE must have a SCHEMA section: %v", argStrings(args))
	}
	if !containsSequence(args, "chat", "TAG") {
		t.Errorf("FT.CREATE schema must declare `chat` as TAG: %v", argStrings(args))
	}
	if !containsSequence(args, "DISTANCE_METRIC", vectorDistance) {
		t.Errorf("DISTANCE_METRIC %s missing: %v", vectorDistance, argStrings(args))
	}
	if !containsSequence(args, "DIM", strconv.Itoa(vectorDim)) {
		t.Errorf("DIM %d missing: %v", vectorDim, argStrings(args))
	}
	if !containsSequence(args, "PREFIX", "1", "vfs:vec:") {
		t.Errorf("key PREFIX vfs:vec: missing: %v", argStrings(args))
	}
}

// TestParseSearchResponseMetadataAndScore: FT.SEARCH returns
// [count, key, [field, value, …], …] with `metadata` as a JSON blob and
// `score` as the KNN distance. Reply values may arrive as []byte.
func TestParseSearchResponseMetadataAndScore(t *testing.T) {
	resp := []interface{}{
		int64(2),
		"vfs:vec:room-1:public/index.html",
		[]interface{}{
			"score", "0.123",
			"metadata", []byte(`{"path":"public/index.html","text":"<html>coffee</html>"}`),
		},
		"vfs:vec:room-1:public/styles.css",
		[]interface{}{
			"score", "0.456",
			"metadata", `{"path":"public/styles.css","text":":root{}"}`,
		},
	}

	got := parseSearchResponse(resp, 6)
	if len(got) != 2 {
		t.Fatalf("results = %d, want 2", len(got))
	}
	if got[0].Path != "public/index.html" || got[0].Text != "<html>coffee</html>" {
		t.Errorf("result 0 = %+v", got[0])
	}
	if math.Abs(got[0].Score-0.123) > 1e-9 {
		t.Errorf("result 0 score = %v, want 0.123", got[0].Score)
	}
	if got[1].Path != "public/styles.css" || math.Abs(got[1].Score-0.456) > 1e-9 {
		t.Errorf("result 1 = %+v", got[1])
	}
}

// TestParseSearchResponseSkipsMalformed: rows without usable metadata are
// dropped rather than producing empty-path results, and k caps the output.
func TestParseSearchResponseSkipsMalformed(t *testing.T) {
	resp := []interface{}{
		int64(3),
		"k1", []interface{}{"score", "1.0"}, // no metadata -> no path
		"k2", []interface{}{"metadata", `{"path":"a.js","text":"x"}`, "score", "0.5"},
		"k3", []interface{}{"metadata", `{"path":"b.js","text":"y"}`, "score", "0.6"},
	}
	if got := parseSearchResponse(resp, 6); len(got) != 2 {
		t.Errorf("results = %d, want 2 (malformed row dropped)", len(got))
	}
	if got := parseSearchResponse(resp, 1); len(got) != 1 {
		t.Errorf("results = %d, want 1 (capped by k)", len(got))
	}
	if got := parseSearchResponse("not an array", 3); got != nil {
		t.Errorf("non-array reply must yield nil, got %v", got)
	}
	if got := parseSearchResponse([]interface{}{int64(0)}, 3); got != nil {
		t.Errorf("empty result set must yield nil, got %v", got)
	}
}

// TestParseScoreNormalizesNaN: RediSearch emits "-nan" when the cosine
// distance is undefined (zero vector). It must not become a Go NaN, which
// would make every score comparison false and scramble result ordering.
func TestParseScoreNormalizesNaN(t *testing.T) {
	cases := map[string]float64{
		"0.25":    0.25,
		"0":       0,
		"-nan":    0,
		"nan":     0,
		"inf":     0,
		"-inf":    0,
		"":        0,
		"garbage": 0,
	}
	for in, want := range cases {
		got := parseScore(in)
		if math.IsNaN(got) || math.Abs(got-want) > 1e-9 {
			t.Errorf("parseScore(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestHasMagnitude: an embedding with no direction makes cosine distance
// undefined, so SearchVFS must skip the lookup entirely.
func TestHasMagnitude(t *testing.T) {
	if hasMagnitude(HashEmbedding("")) {
		t.Error("an empty query must produce a zero embedding")
	}
	if hasMagnitude([]float32{0, 0, 0}) {
		t.Error("an all-zero vector has no magnitude")
	}
	if !hasMagnitude(HashEmbedding("coffee landing hero")) {
		t.Error("a non-empty text must produce a non-zero embedding")
	}
	if !hasMagnitude([]float32{0, 0, 0.5}) {
		t.Error("one non-zero component is enough")
	}
}

// TestHashEmbeddingIsUnitLength: COSINE distance is only comparable across
// files when every vector is normalized.
func TestHashEmbeddingIsUnitLength(t *testing.T) {
	emb := HashEmbedding("public/index.html coffee landing hero section")
	if len(emb) != vectorDim {
		t.Fatalf("dim = %d, want %d", len(emb), vectorDim)
	}
	sum := 0.0
	for _, v := range emb {
		sum += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(sum)-1.0) > 1e-5 {
		t.Errorf("embedding is not unit length: |v| = %v", math.Sqrt(sum))
	}
	if raw := Float32ToBytes(emb); len(raw) != vectorDim*4 {
		t.Errorf("serialized blob = %d bytes, want %d", len(raw), vectorDim*4)
	}
}

// TestPreviewCutsAtRuneBoundary: the stored text is re-injected into an LLM
// prompt, so splitting a multi-byte character would corrupt the JSON.
func TestPreviewCutsAtRuneBoundary(t *testing.T) {
	if got := preview("short", 10); got != "short" {
		t.Errorf("preview of a short string = %q", got)
	}
	// "قهوه" is 8 bytes (four 2-byte runes); cutting at n=5 lands inside the
	// third rune and must back off to the last rune boundary (4 bytes).
	got := preview("قهوه", 5)
	if got != "قه" || !utf8.ValidString(got) {
		t.Errorf("preview cut = %q (%d bytes), want %q and valid UTF-8", got, len(got), "قه")
	}
	// ASCII needs no back-off.
	if got := preview("abcdef", 3); got != "abc" {
		t.Errorf("ascii preview = %q, want %q", got, "abc")
	}
}

// TestVectorKeyScopesPerRoom: two rooms writing the same relative path must
// not overwrite each other's embedding.
func TestVectorKeyScopesPerRoom(t *testing.T) {
	a := vectorKey("room-a", "public/index.html")
	b := vectorKey("room-b", "public/index.html")
	if a == b {
		t.Fatalf("vector keys collide across rooms: %q", a)
	}
	if !strings.HasPrefix(a, "vfs:vec:room-a:") || !strings.HasSuffix(a, "public/index.html") {
		t.Errorf("key = %q, want the vfs:vec:<chatID>:<path> shape", a)
	}
}

// TestRoomSearchVFSWithoutRedis: no Redis client means no error and no
// results — the RAG context simply stays empty (greenfield behaviour).
func TestRoomSearchVFSWithoutRedis(t *testing.T) {
	room := NewProjectRoom("vec-noredis", nil, nil, nil)
	res, err := room.SearchVFS(context.Background(), "coffee landing", 3)
	if err != nil {
		t.Fatalf("SearchVFS without Redis: %v", err)
	}
	if res != nil {
		t.Errorf("results = %v, want nil", res)
	}
	if ctxStr := room.BuildRAGContext(context.Background(), "coffee landing", 3); ctxStr != "" {
		t.Errorf("BuildRAGContext without Redis = %q, want empty", ctxStr)
	}
}
