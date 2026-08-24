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
func EnsureVectorIndex(ctx context.Context, rdb *redis.Client) error {
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

// buildCreateArgs returns the full argument list for FT.CREATE.
func buildCreateArgs() []interface{} {
	return []interface{}{
		"FT.CREATE", vectorIndexName,
		"ON", "HASH",
		"PREFIX", "1", "vfs:vec:",
		"SCHEMA", "embedding",
		"VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", vectorDim,
		"DISTANCE_METRIC", vectorDistance,
	}
}

// createCmdArgs assembles the FT.CREATE command arguments from the raw
// string, splitting on spaces.
func createCmdArgs(index, dim string, metric string) []interface{} {
	// Build the schema portion verbatim from the declared constants.
	args := []interface{}{
		index, "ON", "HASH", "PREFIX", "1", "vfs:vec:",
		"SCHEMA", "embedding",
		"VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", dim,
		"DISTANCE_METRIC", metric,
	}
	return args
}

func isIndexAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Index already exists") || strings.Contains(s, "Index already exists")
}

// vectorKey is 'vfs:vec:{filePath}' — the Redis key holding one hash with
// the embedding (binary) and the metadata (path + text preview).
func vectorKey(filePath string) string {
	return "vfs:vec:" + filePath
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
func (r *ProjectRoom) IndexFile(filePath, contents string) error {
	if r.rdb == nil {
		return nil
	}
	emb := HashEmbedding(contents)
	payload, _ := json.Marshal(map[string]string{
		"path": filePath,
		"text": preview(contents, 400),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return r.rdb.HSet(ctx, vectorKey(filePath),
		"embedding", Float32ToBytes(emb),
		"metadata", string(payload),
	).Err()
}

// ReindexVFS iterates the room's VFS and (re)indexes every file.
func (r *ProjectRoom) ReindexVFS() error {
	files := r.GetVFS()
	for path, f := range files {
		if err := r.IndexFile(path, f.FileContents); err != nil {
			log.Printf("[vector:%s] index %s: %v", r.chatID, path, err)
		}
	}
	return nil
}

// SearchResult is one KNN hit from the vector index.
type SearchResult struct {
	Path  string  `json:"path"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// SearchVFS runs a KNN query against 'idx:vfs' for the given query text.
// It returns the top-k most relevant file chunks.
func (r *ProjectRoom) SearchVFS(ctx context.Context, query string, k int) ([]SearchResult, error) {
	if r.rdb == nil || k <= 0 {
		return nil, nil
	}

	blob := Float32ToBytes(HashEmbedding(query))

	// Build args: FT.SEARCH idx:vfs "*=>[KNN $k @embedding $BLOB]" PARAMS 4 BLOB <blob> k <k> ...
	knnFilter := "*=>[KNN " + strconv.Itoa(k) + " @embedding $BLOB]"
	args := []interface{}{
		"FT.SEARCH", vectorIndexName, knnFilter,
		"PARAMS", "4", "BLOB", blob, "k", strconv.Itoa(k),
		"RETURN", "2", "path", "text",
		"SORTBY", "__embedding_score", "DESC",
	}
	resp, err := r.rdb.Do(ctx, args...).Result()
	if err != nil {
		if strings.Contains(err.Error(), "No such index") {
			return nil, nil
		}
		return nil, err
	}

	return parseSearchResponse(resp, k), nil
}

// parseSearchResponse interprets the raw Array reply from FT.SEARCH.
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
			field, _ := fields[j].(string)
			val, _ := fields[j+1].(string)
			switch field {
			case "metadata":
				var md map[string]string
				_ = json.Unmarshal([]byte(val), &md)
				sr.Path = md["path"]
				sr.Text = md["text"]
			case "__embedding_score":
				fmt.Sscanf(val, "%f", &sr.Score)
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

// BuildRAGContext queries the vector index for chunks relevant to prompt,
// and returns a formatted context string to inject into the LLM prompt.
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
	return sb.String()
}

// preview returns the first n chars of s.
func preview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
