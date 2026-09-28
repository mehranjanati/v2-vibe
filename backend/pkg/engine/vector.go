package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

// Vector configuration. The index is named 'idx:vfs' as required, uses
// HNSW for ANN search over 256-dim float vectors.
const (
	vectorIndexName = "idx:vfs"
	vectorDim       = 256
	vectorDistance  = "COSINE" // normalized cosine similarity
)

// EnsureVectorIndex creates the 'idx:vfs' Redis vector index if it does
// not already exist. Call once at startup.
//
// An index left over from an older schema (no per-room `chat` tag) is
// dropped and recreated: without that tag every project shares one global
// namespace, so a search returns other projects' files as "existing code
// context" and the planner plans against the wrong codebase.
func EnsureVectorIndex(ctx context.Context, rdb *redis.Client) error {
	if stale, err := indexNeedsRecreate(ctx, rdb); err != nil {
		log.Printf("[vector] schema check failed: %v", err)
	} else if stale {
		log.Printf("[vector] dropping stale index %s (schema without chat tag)", vectorIndexName)
		if err := rdb.Do(ctx, "FT.DROPINDEX", vectorIndexName).Err(); err != nil {
			return fmt.Errorf("drop stale index: %w", err)
		}
	}

	args := buildCreateArgs()
	if err := rdb.Do(ctx, args...).Err(); err != nil {
		// Redis returns "Index already exists" when it exists; treat that as ok.
		if isIndexAlreadyExists(err) {
			log.Println("[vector] index already exists")
			return nil
		}
		return err
	}
	log.Printf("[vector] created Redis vector index %s", vectorIndexName)
	return nil
}

// indexNeedsRecreate reports whether 'idx:vfs' exists but lacks the `chat`
// tag field, i.e. it was created by an older schema version.
func indexNeedsRecreate(ctx context.Context, rdb *redis.Client) (bool, error) {
	resp, err := rdb.Do(ctx, "FT.INFO", vectorIndexName).Result()
	if err != nil {
		// No such index — nothing to migrate.
		if strings.Contains(err.Error(), "Unknown Index name") ||
			strings.Contains(err.Error(), "no such index") {
			return false, nil
		}
		return false, err
	}
	attrs, ok := flatInfoValue(resp, "attributes")
	if !ok {
		return false, nil
	}
	list, ok := attrs.([]interface{})
	if !ok {
		return false, nil
	}
	for _, a := range list {
		fields, ok := a.([]interface{})
		if !ok {
			continue
		}
		for i := 0; i+1 < len(fields); i += 2 {
			key, _ := fields[i].(string)
			val, _ := fields[i+1].(string)
			if (key == "attribute" || key == "identifier") && val == "chat" {
				return false, nil
			}
		}
	}
	return true, nil
}

// flatInfoValue finds `key` in a flat FT.INFO reply ([k1 v1 k2 v2 …]).
func flatInfoValue(resp interface{}, key string) (interface{}, bool) {
	arr, ok := resp.([]interface{})
	if !ok {
		return nil, false
	}
	for i := 0; i+1 < len(arr); i += 2 {
		if k, _ := arr[i].(string); k == key {
			return arr[i+1], true
		}
	}
	return nil, false
}

// buildCreateArgs returns the full argument list for FT.CREATE.
//
// The `chat` TAG field scopes every search to one project room; without it
// `vfs:vec:*` keys from every room collide on the same file paths and the
// RAG context mixes unrelated projects together.
func buildCreateArgs() []interface{} {
	return []interface{}{
		"FT.CREATE", vectorIndexName,
		"ON", "HASH",
		"PREFIX", "1", "vfs:vec:",
		"SCHEMA",
		"chat", "TAG", "SEPARATOR", "|",
		"embedding",
		"VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		// DIM is emitted as an explicit string, like every other argument:
		// passing the raw int would depend on the driver's arg coercion.
		"DIM", strconv.Itoa(vectorDim),
		"DISTANCE_METRIC", vectorDistance,
	}
}

func isIndexAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Index already exists")
}

// vectorKey is 'vfs:vec:{chatID}:{filePath}' — the Redis key holding one
// hash with the embedding (binary), the room tag and the metadata
// (path + text preview).
//
// The chatID segment is part of the key, not just of the hash: without it
// two different projects both writing `public/index.html` overwrite each
// other's embedding, so the room loses its own index entries entirely.
func vectorKey(chatID, filePath string) string {
	return "vfs:vec:" + chatID + ":" + filePath
}

// HashEmbedding computes a deterministic 256-dim float32 vector from the
// text using character n-grams. This is a lightweight local stand-in for a
// model-based embedding API; swap the implementation for a real embedding
// endpoint (e.g. Cloudflare Workers AI) without changing callers.
func HashEmbedding(text string) []float32 {
	vec := make([]float32, vectorDim)
	ngrams := extractNGrams(text, 3)
	for _, ng := range ngrams {
		h := fnv.New64a()
		_, _ = h.Write([]byte(ng))
		idx := h.Sum64() % uint64(vectorDim)
		vec[idx]++
	}
	// Normalize to unit length.
	norm := 0.0
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		n := math.Sqrt(norm)
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / n)
		}
	}
	return vec
}

// extractNGrams splits text into byte n-grams (skipping whitespace).
func extractNGrams(s string, n int) []string {
	compact := strings.NewReplacer("\n", " ", "\t", " ", "\r", " ").Replace(s)
	var out []string
	for i := 0; i+n <= len(compact); i++ {
		out = append(out, compact[i:i+n])
	}
	return out
}

// Float32ToBytes serializes a []float32 to little-endian bytes for Redis.
func Float32ToBytes(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		b[i*4+0] = byte(bits)
		b[i*4+1] = byte(bits >> 8)
		b[i*4+2] = byte(bits >> 16)
		b[i*4+3] = byte(bits >> 24)
	}
	return b
}

// IndexFile embeds the file content and upserts the vector hash into Redis.
//
// The hash carries the room's chatID as a TAG so searches are scoped to
// this project: a global index returns other rooms' files as "existing
// code context", which makes the planner plan against the wrong codebase.
func (r *ProjectRoom) IndexFile(filePath, contents string) error {
	if r.rdb == nil {
		return nil
	}
	emb := HashEmbedding(contents)
	payload, _ := json.Marshal(map[string]string{
		"path": filePath,
		"text": preview(contents, vectorTextPreviewChars),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return r.rdb.HSet(ctx, vectorKey(r.chatID, filePath),
		"chat", r.chatID,
		"embedding", Float32ToBytes(emb),
		"metadata", string(payload),
	).Err()
}

// ReindexVFS iterates the room's VFS and (re)indexes every file.
func (r *ProjectRoom) ReindexVFS() error {
	files := r.GetVFS()
	indexed := 0
	for path, f := range files {
		if err := r.IndexFile(path, f.FileContents); err != nil {
			log.Printf("[vector:%s] index %s: %v", r.chatID, path, err)
			continue
		}
		indexed++
	}
	if indexed > 0 {
		log.Printf("[vector:%s] reindexed %d file(s)", r.chatID, indexed)
	}
	return nil
}

// vectorTextPreviewChars caps how much of a file is stored in the `metadata`
// JSON. This is what the planner actually reads as "Relevant existing code
// context", so it must be large enough to show real structure (imports,
// exported names, the top of the markup) but small enough that k hits never
// blow up the planner prompt.
const vectorTextPreviewChars = 1200

// SearchResult is one KNN hit from the vector index.
type SearchResult struct {
	Path  string  `json:"path"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// SearchVFS runs a KNN query against 'idx:vfs' for the given query text,
// scoped to this room's files only. It returns the top-k most relevant
// file chunks.
//
// Three details matter for RediSearch here, all of which the previous
// implementation got wrong (every call failed with
// "Syntax error at offset 1 near >["):
//
//  1. The `*=>[KNN …]` vector query syntax only parses under DIALECT >= 2;
//     without the explicit `DIALECT 2` argument the server falls back to
//     dialect 1 and rejects the query.
//  2. The distance alias must be created in the query itself
//     (`$BLOB AS score`). There is no implicit `__embedding_score` field,
//     so sorting by it fails. COSINE distance is 0 for identical vectors,
//     so ASC = most similar first.
//  3. RETURN must name real hash fields. The hash holds `metadata` (a JSON
//     blob with path+text) and `score` — not flat `path`/`text` fields.
//
// A nil blob (all-zero embedding, e.g. an empty query) makes RediSearch
// return -nan scores and arbitrary ordering, so it is skipped up front.
func (r *ProjectRoom) SearchVFS(ctx context.Context, query string, k int) ([]SearchResult, error) {
	if r.rdb == nil || k <= 0 {
		return nil, nil
	}
	emb := HashEmbedding(query)
	if !hasMagnitude(emb) {
		// An all-zero embedding makes cosine distance undefined; the
		// server answers with -nan scores in arbitrary order, which
		// would inject unrelated files into the planner context.
		return nil, nil
	}

	args := buildSearchArgs(r.chatID, Float32ToBytes(emb), k)
	resp, err := r.rdb.Do(ctx, args...).Result()
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "No such index") || strings.Contains(msg, "Unknown Index name") {
			return nil, nil
		}
		return nil, err
	}

	return parseSearchResponse(resp, k), nil
}

// buildSearchArgs assembles the FT.SEARCH argument list for a room-scoped
// KNN query. It is a pure function so the exact command the server sees is
// covered by a unit test — getting any one of the three dialect/alias/RETURN
// details wrong makes every RAG lookup fail silently.
func buildSearchArgs(chatID string, blob []byte, k int) []interface{} {
	knnFilter := "@chat:{" + escapeTag(chatID) + "}=>[KNN " +
		strconv.Itoa(k) + " @embedding $BLOB AS score]"
	return []interface{}{
		"FT.SEARCH", vectorIndexName, knnFilter,
		"PARAMS", "2", "BLOB", blob,
		"SORTBY", "score", "ASC",
		"RETURN", "2", "metadata", "score",
		"DIALECT", "2",
	}
}

// hasMagnitude reports whether the embedding has any non-zero component.
// An all-zero vector has no direction, so cosine distance is undefined and
// RediSearch answers with -nan scores in arbitrary order.
func hasMagnitude(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return true
		}
	}
	return false
}

// escapeTag neutralizes the RediSearch TAG-query metacharacters in a chat
// ID. IDs are UUIDs or caller-supplied slugs (e.g. "carstore-1788777330553"),
// but a user-controlled ID containing `-` or `@` would otherwise alter the
// query.
func escapeTag(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		switch c {
		case '@', '{', '}', '|', '-', ':', ',', '.', ' ', '*', '?', '~', '!', '$', '^', '[', ']', '(', ')', '=', '+', '&', '/', '\\', '"', '\'':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// parseSearchResponse interprets the raw Array reply from FT.SEARCH:
// [count, key, [field, value, …], key, [field, value, …], …].
//
// The requested fields are `metadata` (the JSON blob IndexFile wrote) and
// `score` (the KNN distance alias created by the query). Values arrive as
// either string or []byte depending on the driver path, so both are
// accepted; a -nan score (undefined cosine distance) is normalized to 0.
func parseSearchResponse(resp interface{}, k int) []SearchResult {
	arr, ok := resp.([]interface{})
	if !ok || len(arr) < 1 {
		return nil
	}
	// arr[0] is total count
	var results []SearchResult
	for i := 1; i < len(arr); i += 2 {
		if i+1 >= len(arr) {
			break
		}
		// Each result: key then a flat list of field/value pairs.
		fields, ok := arr[i+1].([]interface{})
		if !ok {
			continue
		}
		sr := SearchResult{}
		for j := 0; j+1 < len(fields); j += 2 {
			field := respString(fields[j])
			val := respString(fields[j+1])
			switch field {
			case "metadata":
				var md map[string]string
				_ = json.Unmarshal([]byte(val), &md)
				sr.Path = md["path"]
				sr.Text = md["text"]
			case "score":
				sr.Score = parseScore(val)
			}
		}
		if sr.Path != "" {
			results = append(results, sr)
		}
		if len(results) >= k {
			break
		}
	}
	return results
}

// respString coerces one FT.SEARCH reply element to a string.
func respString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

// parseScore reads a cosine distance value, mapping the -nan/+nan/inf the
// server emits for an undefined distance onto 0 (i.e. "no signal").
func parseScore(s string) float64 {
	var f float64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f); err != nil {
		return 0
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

// BuildRAGContext queries the vector index for chunks relevant to prompt,
// and returns a formatted context string to inject into the LLM prompt.
//
// An empty string means "no usable context": either the index is empty (a
// greenfield build) or the query produced no embedding. Callers append the
// result only when non-empty.
func (r *ProjectRoom) BuildRAGContext(ctx context.Context, prompt string, k int) string {
	results, err := r.SearchVFS(ctx, prompt, k)
	if err != nil {
		log.Printf("[rag:%s] search: %v", r.chatID, err)
		return ""
	}
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Relevant existing code context from the project VFS:\n")
	for i, res := range results {
		fmt.Fprintf(&sb, "\n[%d] %s\n%s\n", i+1, res.Path, res.Text)
	}
	log.Printf("[rag:%s] injected %d file chunk(s) into the planner context", r.chatID, len(results))
	return sb.String()
}

// preview returns the first n chars of s, cut at a rune boundary so a
// multi-byte character is never split in half (the stored text is read
// back into an LLM prompt as JSON).
func preview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
