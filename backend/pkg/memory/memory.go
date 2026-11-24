package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Scope is where a memory lives on the containment ladder, narrowest first.
type Scope string

const (
	ScopeRun          Scope = "run"
	ScopeConversation Scope = "conversation"
	ScopeAgent        Scope = "agent"
	ScopeTeam         Scope = "team"
	ScopeTenant       Scope = "tenant"
)

// ladder is the containment order used for read authority.
var ladder = []Scope{ScopeRun, ScopeConversation, ScopeAgent, ScopeTeam, ScopeTenant}

// Source says where a record came from. The explicit/inferred split is the one
// that must survive storage.
type Source string

const (
	SourceExplicit   Source = "explicit"
	SourceObserved   Source = "observed"
	SourceInferred   Source = "inferred"
	SourceToolResult Source = "tool-result"
	SourceSystem     Source = "system"
)

// Kinds of memory record.
const (
	KindFact       = "fact"
	KindPreference = "preference"
	KindSummary    = "summary"
	KindEventNote  = "event-note"
)

// Default provider bounds. A provider may advertise less, never more.
const (
	DefaultMaxItems  = 50
	DefaultMaxTokens = 4000
)

var (
	// ErrTenant means the tenant is missing or the operation would cross tenants.
	ErrTenant = errors.New("memory: tenant is required and defines a wall")
	// ErrScope means the requested scope is outside the caller's authority.
	ErrScope = errors.New("memory: scope is outside the caller's authority")
	// ErrSecret means the record carries a literal credential.
	ErrSecret = errors.New("memory: record carries a literal credential; use a vault: reference")
	// ErrBounds means the request is outside the provider's advertised capability.
	ErrBounds = errors.New("memory: request exceeds the provider's advertised bounds")
	// ErrRetention means an expiring write was sent to a provider without TTL support.
	ErrRetention = errors.New("memory: provider cannot honour an expiring write")
	// ErrConflict means an inferred record targeted a key that has an explicit value.
	ErrConflict = errors.New("memory: an inferred value cannot overwrite an explicit one")
	// ErrInvalid means the record or query violates the contract.
	ErrInvalid = errors.New("memory: invalid record or query")
)

// Provenance is copied to every record and never inferred at read time.
type Provenance struct {
	Source   Source
	Producer string
	Version  string
}

// Record is one memory.
type Record struct {
	ID         string
	TenantKey  string
	Scope      Scope
	Subject    string
	Kind       string
	Key        string // required for KindPreference, empty otherwise
	Text       string
	Data       map[string]any
	Provenance Provenance
	CreatedAt  time.Time // injected clock
	ExpiresAt  *time.Time
}

// Query is one bounded recall.
type Query struct {
	TenantKey   string
	CallerScope Scope // the caller's own scope; it defines what may be read
	Subject     string
	Scopes      []Scope
	Kind        string
	Key         string
	Text        string
	MaxItems    int
	MaxTokens   int
	// Now is the injected clock. The expiry filter compares against it, so the
	// provider never calls time.Now() and a replay is comparable.
	Now time.Time
}

// Item is a record plus its provider score and estimated cost.
type Item struct {
	Record
	Score  float64
	Tokens int
}

// ForgetRequest removes by id, or by scope+subject.
type ForgetRequest struct {
	TenantKey string
	ID        string
	Scope     Scope
	Subject   string
}

// Capabilities is what a provider can honestly do.
type Capabilities struct {
	SemanticRecall bool
	SupportsTTL    bool
	MaxItems       int
	MaxTokens      int
}

// Provider is the three-operation interface.
type Provider interface {
	Remember(ctx context.Context, record Record) (string, error)
	Recall(ctx context.Context, query Query) ([]Item, error)
	Forget(ctx context.Context, request ForgetRequest) (int, error)
	Capabilities() Capabilities
}

// AllowedScopes returns the scopes a caller at the given scope may read: its own
// scope and every broader one. Reading narrower scopes is not allowed - a
// tenant-scope caller may not reach into a single run.
func AllowedScopes(caller Scope) ([]Scope, error) {
	for i, scope := range ladder {
		if scope == caller {
			return append([]Scope{}, ladder[i:]...), nil
		}
	}
	return nil, fmt.Errorf("%w: unknown caller scope %q", ErrScope, caller)
}

// ValidateRecord enforces the contract on a record.
func ValidateRecord(record Record) error {
	if strings.TrimSpace(record.TenantKey) == "" {
		return fmt.Errorf("%w: tenantKey is required", ErrTenant)
	}
	if _, err := AllowedScopes(record.Scope); err != nil {
		return fmt.Errorf("%w: unknown scope %q", ErrScope, record.Scope)
	}
	if strings.TrimSpace(record.Subject) == "" {
		return fmt.Errorf("%w: subject is required", ErrInvalid)
	}
	if !ValidKind(record.Kind) {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalid, record.Kind)
	}
	if record.Kind == KindPreference && strings.TrimSpace(record.Key) == "" {
		return fmt.Errorf("%w: a preference needs a stable key", ErrInvalid)
	}
	if record.Key != "" && !ValidKey(record.Key) {
		return fmt.Errorf("%w: key %q must be a dotted lowercase identifier", ErrInvalid, record.Key)
	}
	if strings.TrimSpace(record.Text) == "" && len(record.Data) == 0 {
		return fmt.Errorf("%w: a record needs text or data", ErrInvalid)
	}
	if !ValidSource(record.Provenance.Source) {
		return fmt.Errorf("%w: unknown provenance source %q", ErrInvalid, record.Provenance.Source)
	}
	if strings.TrimSpace(record.Provenance.Producer) == "" {
		return fmt.Errorf("%w: provenance producer is required", ErrInvalid)
	}
	if record.CreatedAt.IsZero() {
		return fmt.Errorf("%w: createdAt must come from the injected clock", ErrInvalid)
	}
	if record.ExpiresAt != nil && !record.ExpiresAt.After(record.CreatedAt) {
		return fmt.Errorf("%w: expiresAt must be after createdAt", ErrInvalid)
	}
	if key, bad := ScanSecret(record.Text, record.Data); bad {
		return fmt.Errorf("%w: field %q", ErrSecret, key)
	}
	return nil
}

// ValidateQuery enforces the tenant wall, the scope authority and the bounds.
func ValidateQuery(query Query, caps Capabilities) error {
	if strings.TrimSpace(query.TenantKey) == "" {
		return fmt.Errorf("%w: a recall without a tenant is refused, never answered empty", ErrTenant)
	}
	allowed, err := AllowedScopes(query.CallerScope)
	if err != nil {
		return err
	}
	if len(query.Scopes) == 0 {
		return fmt.Errorf("%w: at least one scope must be requested", ErrInvalid)
	}
	allowedSet := map[Scope]bool{}
	for _, scope := range allowed {
		allowedSet[scope] = true
	}
	for _, scope := range query.Scopes {
		if !allowedSet[scope] {
			return fmt.Errorf("%w: %q is not readable from %q", ErrScope, scope, query.CallerScope)
		}
	}
	if strings.TrimSpace(query.Subject) == "" {
		return fmt.Errorf("%w: subject is required (no cross-subject reads)", ErrInvalid)
	}
	if query.MaxItems <= 0 || query.MaxItems > caps.MaxItems {
		return fmt.Errorf("%w: maxItems %d outside 1..%d", ErrBounds, query.MaxItems, caps.MaxItems)
	}
	if query.Now.IsZero() {
		return fmt.Errorf("%w: now must come from the injected clock", ErrInvalid)
	}
	if query.MaxTokens <= 0 || query.MaxTokens > caps.MaxTokens {
		return fmt.Errorf("%w: maxTokens %d outside 1..%d", ErrBounds, query.MaxTokens, caps.MaxTokens)
	}
	return nil
}

// Compare is the total order of recall results: score, then newest, then id.
// A total order means two runs over the same rows agree, whatever the storage
// returned first.
func Compare(a, b Item) int {
	if a.Score != b.Score {
		if a.Score > b.Score {
			return -1
		}
		return 1
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		if a.CreatedAt.After(b.CreatedAt) {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}

// SortItems orders items with Compare.
func SortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool { return Compare(items[i], items[j]) < 0 })
}

// Score is the lexical relevance of a record for a query: the share of query
// tokens present in the text. An empty query scores zero, which lets the
// created-at tie-break produce "most recent" ordering.
func Score(query, text string) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	haystack := map[string]bool{}
	for token := range tokenize(text) {
		haystack[token] = true
	}
	hits := 0
	for token := range queryTokens {
		if haystack[token] {
			hits++
		}
	}
	return float64(hits) / float64(len(queryTokens))
}

// EstimateTokens is the documented approximation (four characters per token).
// It protects a context window; it is not a tokenizer, and pretending otherwise
// would pin a provider.
func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return (len(trimmed) + 3) / 4
}

// ContentID derives a stable id for a record so that remembering the same content
// twice is idempotent instead of duplicating it.
func ContentID(record Record) string {
	payload, _ := json.Marshal([]any{
		record.TenantKey, string(record.Scope), record.Subject, record.Kind, record.Key,
		record.Text, record.Data, string(record.Provenance.Source), record.Provenance.Producer,
		record.Provenance.Version, record.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
	sum := sha256.Sum256(payload)
	return "mem:" + hex.EncodeToString(sum[:])[:32]
}

// secretKeys mirrors the denylist the resolver applies to manifests: a memory
// record must never store a credential, because it would then be recalled into
// every future context.
var secretKeys = map[string]bool{
	"apikey": true, "api_key": true, "token": true, "password": true,
	"clientsecret": true, "client_secret": true, "secretkey": true,
	"secret": true, "authorization": true, "bearer": true,
}

// ScanSecret reports the first field carrying a non-vault credential.
func ScanSecret(text string, data map[string]any) (string, bool) {
	if strings.Contains(text, "sk-live-") {
		return "text", true
	}
	var offending string
	found := false
	var walk func(any)
	walk = func(node any) {
		if found {
			return
		}
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if secretKeys[strings.ToLower(key)] && !vaultOnly(child) {
					offending, found = key, true
					return
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(data)
	return offending, found
}

func vaultOnly(node any) bool {
	switch value := node.(type) {
	case string:
		return strings.HasPrefix(value, "vault:") || strings.HasPrefix(value, "vault/")
	case map[string]any:
		for _, child := range value {
			if !vaultOnly(child) {
				return false
			}
		}
		return true
	case []any:
		for _, child := range value {
			if !vaultOnly(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func tokenize(text string) map[string]bool {
	out := map[string]bool{}
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out[strings.ToLower(b.String())] = true
			b.Reset()
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// ValidKind reports whether a kind is in the closed vocabulary.
func ValidKind(kind string) bool {
	switch kind {
	case KindFact, KindPreference, KindSummary, KindEventNote:
		return true
	default:
		return false
	}
}

// ValidSource reports whether a provenance source is in the closed vocabulary.
func ValidSource(source Source) bool {
	switch source {
	case SourceExplicit, SourceObserved, SourceInferred, SourceToolResult, SourceSystem:
		return true
	default:
		return false
	}
}

func isKeyByte(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
}

// ValidKey keeps preference keys dotted and lowercase so they stay comparable
// across scopes without a normalisation step.
func ValidKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if !isKeyByte(r) {
			return false
		}
		if i == 0 && !(r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}
