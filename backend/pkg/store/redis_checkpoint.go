// Package store provides durable, out-of-process persistence for the Go
// agent backend. The Go server itself is strictly stateless: every piece
// of conversation state lives in Redis, so instances can be restarted or
// horizontally scaled mid-conversation without losing context.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

// Checkpoint key prefixes stored in Redis.
const (
	// checkpointKeyPrefix holds the serialized conversation history
	// ([]*schema.Message JSON) for a session.
	checkpointKeyPrefix = "agent:checkpoint:"
	// rawCheckpointKeyPrefix holds opaque Eino compose graph checkpoints
	// (interrupt/resume state) for a session.
	rawCheckpointKeyPrefix = "agent:checkpoint:raw:"
	// vfsKeyPrefix holds per-chat Virtual File Systems as Redis hashes
	// (field = file path, value = file contents).
	vfsKeyPrefix = "agent:vfs:"
)

// DefaultCheckpointTTL is how long checkpoints live in Redis. Sessions
// older than this are garbage-collected server-side with zero cost to the
// stateless Go processes.
const DefaultCheckpointTTL = 7 * 24 * time.Hour

// RedisCheckpointStore persists agent session state in Redis. It stores
// the full conversation history per session ID and also satisfies Eino's
// adk.CheckPointStore / compose.CheckPointStore interface for graph-level
// interrupt/resume checkpoints.
//
// The zero-value is unusable; construct with NewRedisCheckpointStore.
type RedisCheckpointStore struct {
	rdb *redis.Client
	ttl time.Duration
}

// compile-time assertion that the store can be used as an Eino checkpoint
// store for interrupt/resume flows.
var _ adk.CheckPointStore = (*RedisCheckpointStore)(nil)

// NewRedisCheckpointStore creates a checkpoint store backed by rdb.
func NewRedisCheckpointStore(rdb *redis.Client) *RedisCheckpointStore {
	return &RedisCheckpointStore{rdb: rdb, ttl: DefaultCheckpointTTL}
}

// NewRedisCheckpointStoreWithTTL creates a checkpoint store with a custom
// TTL for checkpoint expiry.
func NewRedisCheckpointStoreWithTTL(rdb *redis.Client, ttl time.Duration) *RedisCheckpointStore {
	if ttl <= 0 {
		ttl = DefaultCheckpointTTL
	}
	return &RedisCheckpointStore{rdb: rdb, ttl: ttl}
}

func checkpointKey(sessionID string) string {
	return checkpointKeyPrefix + sessionID
}

// SaveCheckpoint persists the conversation state (message history) for
// sessionID, replacing any previous checkpoint. The Go process keeps no
// copy after this call returns.
func (s *RedisCheckpointStore) SaveCheckpoint(ctx context.Context, sessionID string, state []*schema.Message) error {
	if sessionID == "" {
		return fmt.Errorf("store: SaveCheckpoint: empty session ID")
	}
	b, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("store: marshal checkpoint: %w", err)
	}
	if err := s.rdb.Set(ctx, checkpointKey(sessionID), b, s.ttl).Err(); err != nil {
		return fmt.Errorf("store: save checkpoint %q: %w", sessionID, err)
	}
	return nil
}

// LoadCheckpoint retrieves the conversation state for sessionID. When no
// checkpoint exists it returns (nil, nil) — a fresh conversation.
func (s *RedisCheckpointStore) LoadCheckpoint(ctx context.Context, sessionID string) ([]*schema.Message, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("store: LoadCheckpoint: empty session ID")
	}
	b, err := s.rdb.Get(ctx, checkpointKey(sessionID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load checkpoint %q: %w", sessionID, err)
	}
	var state []*schema.Message
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("store: unmarshal checkpoint %q: %w", sessionID, err)
	}
	return state, nil
}

// DeleteCheckpoint removes the conversation checkpoint for sessionID
// (used by clear-conversation flows).
func (s *RedisCheckpointStore) DeleteCheckpoint(ctx context.Context, sessionID string) error {
	return s.rdb.Del(ctx, checkpointKey(sessionID)).Err()
}

// --- Eino adk.CheckPointStore (graph interrupt/resume) implementation ---

// Get implements adk.CheckPointStore: fetch a raw graph checkpoint.
func (s *RedisCheckpointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	key := rawCheckpointKeyPrefix + checkPointID
	b, err := s.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("store: get raw checkpoint %q: %w", checkPointID, err)
	}
	return b, true, nil
}

// Set implements adk.CheckPointStore: persist a raw graph checkpoint.
func (s *RedisCheckpointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	key := rawCheckpointKeyPrefix + checkPointID
	if err := s.rdb.Set(ctx, key, checkPoint, s.ttl).Err(); err != nil {
		return fmt.Errorf("store: set raw checkpoint %q: %w", checkPointID, err)
	}
	return nil
}

// --- Virtual File System (VFS) stored in Redis ---

func vfsKey(chatID string) string { return vfsKeyPrefix + chatID }

// VFSWrite writes (or overwrites) a file in the chat's virtual file system.
func (s *RedisCheckpointStore) VFSWrite(ctx context.Context, chatID, path, content string) error {
	if path == "" {
		return fmt.Errorf("store: VFSWrite: empty path")
	}
	return s.rdb.HSet(ctx, vfsKey(chatID), path, content).Err()
}

// VFSRead returns the contents of a file, or ("", false, nil) when absent.
func (s *RedisCheckpointStore) VFSRead(ctx context.Context, chatID, path string) (string, bool, error) {
	v, err := s.rdb.HGet(ctx, vfsKey(chatID), path).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: VFSRead %q: %w", path, err)
	}
	return v, true, nil
}

// VFSDelete removes a file from the chat's virtual file system.
func (s *RedisCheckpointStore) VFSDelete(ctx context.Context, chatID, path string) error {
	return s.rdb.HDel(ctx, vfsKey(chatID), path).Err()
}

// VFSList returns all file paths currently in the chat's virtual file
// system.
func (s *RedisCheckpointStore) VFSList(ctx context.Context, chatID string) ([]string, error) {
	return s.rdb.HKeys(ctx, vfsKey(chatID)).Result()
}

// VFSListWithContents returns path -> content for every file in the chat's
// VFS (used to materialize snapshots for deployment/preview).
func (s *RedisCheckpointStore) VFSListWithContents(ctx context.Context, chatID string) (map[string]string, error) {
	return s.rdb.HGetAll(ctx, vfsKey(chatID)).Result()
}
