package composition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

var (
	// ErrAssetNotFound means the reference does not exist at the pinned version.
	// It is deliberately distinct from ErrSourceUnavailable so the resolver can
	// tell a resolution failure (E_MISSING) from infrastructure (E_SOURCE).
	ErrAssetNotFound = errors.New("composition: asset not found")
	// ErrSourceUnavailable means the source could not answer right now.
	ErrSourceUnavailable = errors.New("composition: source unavailable")
	// ErrNotAnAsset marks a document that is not a resolvable asset (a System
	// manifest is a composition root, and reserved catalogs have no schema).
	ErrNotAnAsset = errors.New("composition: document is not a resolvable asset")
)

// Asset is one resolvable artifact as the resolver sees it. Bytes are stored
// bytes: the resolver canonicalizes them itself, so an adapter never decides
// what a digest covers.
type Asset struct {
	Ref       string
	Kind      string
	Publisher string
	Bytes     []byte
	Refs      []string
}

// AssetSource is the only I/O seam in the resolver (docs/architecture/composition-resolver.md §2).
type AssetSource interface {
	Fetch(ctx context.Context, ref string) (Asset, error)
}

// BundledSource resolves references against an explicit, in-memory asset set.
// It is how the first slice proves closure shape without a registry: nothing is
// fetched over a network, and an asset that is not in the set is E_MISSING.
type BundledSource struct {
	assets map[string]Asset
}

// NewBundledSource builds a source from explicit assets. Duplicate refs and
// malformed refs are refused at construction, because a source that can answer
// two different things for one ref would make the lock meaningless.
func NewBundledSource(assets ...Asset) (*BundledSource, error) {
	set := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		if _, err := ParseRef(asset.Ref); err != nil {
			return nil, err
		}
		if asset.Kind == "" {
			parsed, _ := ParseRef(asset.Ref)
			asset.Kind = parsed.Kind
		}
		if _, exists := set[asset.Ref]; exists {
			return nil, fmt.Errorf("composition: duplicate asset %q in the bundled set", asset.Ref)
		}
		asset.Refs = sortedCopy(asset.Refs)
		set[asset.Ref] = asset
	}
	return &BundledSource{assets: set}, nil
}

// LoadBundledSource reads every *.json asset in dir. Agent, Skill and Tool
// manifests are parsed (identity from metadata, edges from spec); reserved
// catalogs have no manifest schema in v1 and must be supplied with
// NewBundledSource instead.
func LoadBundledSource(fsys fs.FS, dir string) (*BundledSource, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("composition: read bundled dir %q: %w", dir, err)
	}
	var assets []Asset
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("composition: read %q: %w", entry.Name(), err)
		}
		asset, err := ParseAsset(data)
		if errors.Is(err, ErrNotAnAsset) {
			// A System is a composition root, not a resolvable asset: the bundled
			// directory legitimately contains one (inbound-sales.system.json).
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("composition: %s: %w", entry.Name(), err)
		}
		assets = append(assets, asset)
	}
	return NewBundledSource(assets...)
}

// Assets returns a copy of the bundled set, sorted by ref, so a caller can
// compose sets (for example: real examples plus a few catalog stubs) without
// reaching into internal state.
func (s *BundledSource) Assets() []Asset {
	refs := make([]string, 0, len(s.assets))
	for ref := range s.assets {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := make([]Asset, 0, len(refs))
	for _, ref := range refs {
		out = append(out, s.assets[ref])
	}
	return out
}

// Fetch returns the asset at exactly the requested reference.
func (s *BundledSource) Fetch(_ context.Context, ref string) (Asset, error) {
	asset, ok := s.assets[ref]
	if !ok {
		return Asset{}, fmt.Errorf("%w: %s", ErrAssetNotFound, ref)
	}
	return asset, nil
}

// manifest is the part of a manifest the resolver needs. Everything else stays
// opaque bytes; the resolver is not a manifest validator (ir.Load is).
type manifest struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Publisher string `json:"publisher"`
	} `json:"metadata"`
	Spec json.RawMessage `json:"spec"`
}

// ParseAsset turns manifest bytes into an Asset: identity from metadata and
// dependency edges from the fields the design names as edges.
func ParseAsset(data []byte) (Asset, error) {
	var doc manifest
	if err := json.Unmarshal(data, &doc); err != nil {
		return Asset{}, fmt.Errorf("not a JSON manifest: %w", err)
	}
	kind := strings.ToLower(doc.Kind)
	switch kind {
	case KindAgent, KindSkill, KindTool:
	default:
		return Asset{}, fmt.Errorf("%w: kind %q (reserved catalogs must be supplied explicitly)", ErrNotAnAsset, doc.Kind)
	}
	ref := kind + "/" + doc.Metadata.Name + "@" + doc.Metadata.Version
	if _, err := ParseRef(ref); err != nil {
		return Asset{}, err
	}
	refs, err := extractRefs(kind, doc.Spec)
	if err != nil {
		return Asset{}, err
	}
	return Asset{
		Ref:       ref,
		Kind:      kind,
		Publisher: doc.Metadata.Publisher,
		Bytes:     data,
		Refs:      refs,
	}, nil
}

// extractRefs lists an asset's dependency edges.
//
// The table is deliberately narrow, and two exclusions matter:
//   - Skill.spec.compatibility.agents is a compatibility STATEMENT, not a
//     dependency. Treating it as an edge would invent the cycle
//     agent -> skill -> agent that the examples would otherwise show.
//   - Tool.spec.secrets holds vault: references to credentials, not capability
//     references.
func extractRefs(kind string, raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("spec is not an object: %w", err)
	}
	var refs []string
	switch kind {
	case KindAgent:
		refs = append(refs, stringsAt(spec, "tools")...)
		refs = append(refs, stringsAt(spec, "skills")...)
		refs = append(refs, stringsAt(spec, "knowledge")...)
		if model, ok := spec["model"].(map[string]any); ok {
			if ref, ok := model["ref"].(string); ok && ref != "" {
				refs = append(refs, ref)
			}
			if policy, ok := model["policy"].(map[string]any); ok {
				if fallback, ok := policy["fallback"].(string); ok && fallback != "" {
					refs = append(refs, fallback)
				}
			}
		}
		if evaluation, ok := spec["evaluation"].(map[string]any); ok {
			if dataset, ok := evaluation["dataset"].(string); ok && dataset != "" {
				refs = append(refs, dataset)
			}
		}
	case KindSkill:
		if requires, ok := spec["requires"].(map[string]any); ok {
			refs = append(refs, stringsAt(requires, "capabilities")...)
		}
	case KindTool:
		// No capability edges in v1: a tool reaches the outside world through
		// mcp.Gate and vault: references, neither of which is a closure member.
	}
	return sortedCopy(refs), nil
}

// secretKeys are field names that must never carry a literal credential in a
// manifest. `secrets` (plural) is absent on purpose: the Tool schema uses it for
// vault: DECLARATIONS, which are exactly what the resolver wants to see.
var secretKeys = map[string]bool{
	"apikey": true, "api_key": true, "token": true, "password": true,
	"clientsecret": true, "client_secret": true, "secretkey": true,
	"secret": true, "authorization": true, "bearer": true,
}

// ScanForSecret reports the first field that carries a non-vault credential.
// The check is intentionally shallow and total: it walks every string and
// object, so a literal cannot hide behind nesting.
func ScanForSecret(data []byte) (string, bool) {
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return "", false
	}
	var offending string
	var walk func(node any) bool
	walk = func(node any) bool {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if secretKeys[strings.ToLower(key)] && !isVaultOnly(child) {
					offending = key
					return true
				}
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range value {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	if walk(generic) {
		return offending, true
	}
	return "", false
}

// isVaultOnly accepts a vault: reference, or a container whose every string
// leaf is one (e.g. {"apiKey": {"ref": "vault/acme/crm"}}).
func isVaultOnly(node any) bool {
	switch value := node.(type) {
	case string:
		return strings.HasPrefix(value, "vault:") || strings.HasPrefix(value, "vault/")
	case map[string]any:
		for _, child := range value {
			if !isVaultOnly(child) {
				return false
			}
		}
		return true
	case []any:
		for _, child := range value {
			if !isVaultOnly(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func stringsAt(m map[string]any, key string) []string {
	values, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string{}, values...)
	sort.Strings(out)
	// Collapse duplicates so an asset that lists the same edge twice does not
	// produce two closure parents.
	deduped := out[:1]
	for _, value := range out[1:] {
		if value != deduped[len(deduped)-1] {
			deduped = append(deduped, value)
		}
	}
	return deduped
}
