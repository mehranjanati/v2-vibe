package vault

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// D1 is the subset of the D1 client the store needs (cloudflare.D1Client
// satisfies it; tests inject an in-memory fake).
type D1 interface {
	Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error)
	Exec(ctx context.Context, sql string, args ...interface{}) (bool, error)
}

// Secret is the caller-facing metadata: a name and timestamps, never a value.
type Secret struct {
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is the tenant-scoped credential store.
type Store struct {
	d1     D1
	sealer *Sealer
	now    func() time.Time
	id     func() string
}

// NewStore wires a store. A nil D1 or sealer means the vault is not configured:
// every operation returns ErrNotConfigured instead of pretending.
func NewStore(d1 D1, sealer *Sealer) (*Store, error) {
	if d1 == nil || sealer == nil {
		return nil, ErrNotConfigured
	}
	return &Store{
		d1:     d1,
		sealer: sealer,
		now:    time.Now,
		id:     func() string { return uuid.NewString() },
	}, nil
}

// SetClock is the deterministic-clock seam for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Put seals and upserts one secret. The plaintext never leaves this call.
func (s *Store) Put(ctx context.Context, userID, name, value string) (Secret, error) {
	if s.d1 == nil || s.sealer == nil {
		return Secret{}, ErrNotConfigured
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Secret{}, errors.New("vault: tenant is required")
	}
	if strings.TrimSpace(value) == "" {
		return Secret{}, errors.New("vault: value is required")
	}
	envelope, err := s.sealer.Seal(userID, name, value)
	if err != nil {
		return Secret{}, err
	}
	nowMS := s.now().UTC().UnixMilli()
	if _, err := s.d1.Exec(ctx,
		`INSERT INTO vault_secrets (id, user_id, name, envelope, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, name) DO UPDATE SET envelope = excluded.envelope, updated_at = excluded.updated_at`,
		s.id(), userID, name, envelope, nowMS, nowMS,
	); err != nil {
		return Secret{}, fmt.Errorf("vault: store secret: %w", err)
	}
	return s.secret(ctx, userID, name)
}

// List returns the tenant's secret names (metadata only, name-ordered).
func (s *Store) List(ctx context.Context, userID string) ([]Secret, error) {
	if s.d1 == nil || s.sealer == nil {
		return nil, ErrNotConfigured
	}
	rows, err := s.d1.Query(ctx,
		`SELECT name, created_at, updated_at FROM vault_secrets WHERE user_id = ? ORDER BY name`,
		strings.TrimSpace(userID),
	)
	if err != nil {
		return nil, fmt.Errorf("vault: list secrets: %w", err)
	}
	out := make([]Secret, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowSecret(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get resolves one secret's plaintext for its tenant.
func (s *Store) Get(ctx context.Context, userID, name string) (string, error) {
	if s.d1 == nil || s.sealer == nil {
		return "", ErrNotConfigured
	}
	if !nameRe.MatchString(name) {
		return "", ErrInvalidName
	}
	rows, err := s.d1.Query(ctx,
		`SELECT envelope FROM vault_secrets WHERE user_id = ? AND name = ?`,
		strings.TrimSpace(userID), name,
	)
	if err != nil {
		return "", fmt.Errorf("vault: read secret: %w", err)
	}
	if len(rows) == 0 {
		return "", ErrNotFound
	}
	envelope, _ := rows[0]["envelope"].(string)
	if envelope == "" {
		return "", ErrNotFound
	}
	return s.sealer.Open(strings.TrimSpace(userID), name, envelope)
}

// Delete removes one secret. Unknown names are a no-op: D1's Exec does not
// report a row count, and "already gone" is the desired end state.
func (s *Store) Delete(ctx context.Context, userID, name string) error {
	if s.d1 == nil || s.sealer == nil {
		return ErrNotConfigured
	}
	if !nameRe.MatchString(name) {
		return ErrInvalidName
	}
	if _, err := s.d1.Exec(ctx,
		`DELETE FROM vault_secrets WHERE user_id = ? AND name = ?`,
		strings.TrimSpace(userID), name,
	); err != nil {
		return fmt.Errorf("vault: delete secret: %w", err)
	}
	return nil
}

// Resolve turns policy references into plaintext values for one tenant, at call
// time only. The map is keyed by the original ref so a caller can label errors.
func (s *Store) Resolve(ctx context.Context, userID string, refs []string) (map[string]string, error) {
	if s.d1 == nil || s.sealer == nil {
		return nil, ErrNotConfigured
	}
	values := make(map[string]string, len(refs))
	for _, ref := range refs {
		name, err := ParseRef(ref)
		if err != nil {
			return nil, err
		}
		value, err := s.Get(ctx, userID, name)
		if err != nil {
			return nil, err
		}
		values[strings.TrimSpace(ref)] = value
	}
	return values, nil
}

func (s *Store) secret(ctx context.Context, userID, name string) (Secret, error) {
	rows, err := s.d1.Query(ctx,
		`SELECT name, created_at, updated_at FROM vault_secrets WHERE user_id = ? AND name = ?`,
		userID, name,
	)
	if err != nil {
		return Secret{}, fmt.Errorf("vault: read secret: %w", err)
	}
	if len(rows) == 0 {
		return Secret{}, ErrNotFound
	}
	return rowSecret(rows[0]), nil
}

func rowSecret(row map[string]interface{}) Secret {
	name, _ := row["name"].(string)
	return Secret{
		Name:      name,
		CreatedAt: millis(row["created_at"]),
		UpdatedAt: millis(row["updated_at"]),
	}
}

func millis(value interface{}) time.Time {
	switch typed := value.(type) {
	case float64:
		return time.UnixMilli(int64(typed)).UTC()
	case int64:
		return time.UnixMilli(typed).UTC()
	default:
		return time.Time{}
	}
}
