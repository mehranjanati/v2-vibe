package openapiimport

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Options pins the import. Publisher, License and a version are required: an
// imported tool without an owner and a version is exactly the unpinned,
// unattributable surface D18 forbids.
type Options struct {
	// Publisher becomes metadata.publisher (the owning org).
	Publisher string
	// License becomes metadata.license (an SPDX tag).
	License string
	// Version pins metadata.version. When empty, info.version is used and
	// must already be MAJOR.MINOR.PATCH.
	Version string
	// Owner and Commit become provenance (repo/commit) when set.
	Owner  string
	Commit string
	// Allow is the audited surface: operationIds and/or "METHOD /path"
	// entries. Anything not listed is not imported.
	Allow []string
	// AllowAll imports every operation. It exists for a deliberate, reviewed
	// bulk import; the default (false) refuses to guess.
	AllowAll bool
	// Server is the Gate registry key these tools belong to. Defaults to
	// `<publisher>-api`.
	Server string
	// TenantContextKey is the argument carrying the tenant. Defaults to
	// "tenant_id".
	TenantContextKey string
	// TimeoutSeconds defaults to 30.
	TimeoutSeconds int
	// ResourceOverride names the permission resource instead of deriving it
	// from the first static path segment (e.g. "leads" for `/v2/leads`).
	ResourceOverride string
}

const (
	defaultTenantKey = "tenant_id"
	defaultTimeout   = 30
	idempotencyTTL   = 86400
)

// credentialHeaderNames are parameter names that must never reach a manifest:
// the value is a secret, so it becomes a vault: reference and is dropped from
// the input schema entirely.
var credentialHeaderNames = map[string]bool{
	"authorization": true, "proxy-authorization": true, "x-api-key": true,
	"api-key": true, "apikey": true, "x-auth-token": true, "x-access-token": true,
	"cookie": true, "set-cookie": true,
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Import maps the allowed operations onto pinned Tool Manifests. Output is
// sorted by tool name, so the same spec and options always produce the same
// bytes.
func Import(spec *Spec, opts Options) ([]Imported, error) {
	if spec == nil {
		return nil, fmt.Errorf("openapiimport: spec is required")
	}
	if err := opts.normalize(spec); err != nil {
		return nil, err
	}
	allow, err := opts.allowSet(spec)
	if err != nil {
		return nil, err
	}

	var out []Imported
	names := map[string]string{}
	for _, path := range sortedKeys(spec.Paths) {
		item := spec.Paths[path]
		for _, m := range item.methods() {
			if m.op == nil {
				continue
			}
			key := strings.ToUpper(m.name) + " " + path
			if !allow.permits(m.name, path, m.op.OperationID) {
				continue
			}
			if m.op.Deprecated {
				return nil, fmt.Errorf("openapiimport: %s (%s): deprecated operations are not importable; exclude it or fix the spec", key, m.op.OperationID)
			}
			imp, err := build(spec, path, m.name, m.op, opts)
			if err != nil {
				return nil, err
			}
			if prev, dup := names[imp.Name()]; dup {
				return nil, fmt.Errorf("openapiimport: two operations map to the tool name %q (%s and %s); set distinct operationIds or exclude one", imp.Name(), prev, key)
			}
			names[imp.Name()] = key
			if err := imp.Validate(); err != nil {
				return nil, err
			}
			out = append(out, imp)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("openapiimport: the allow-list matched no operation (listed: %s)", strings.Join(opts.Allow, ", "))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name() < out[b].Name() })
	return out, nil
}

func (o *Options) normalize(spec *Spec) error {
	o.Publisher = strings.TrimSpace(o.Publisher)
	if o.Publisher == "" {
		return fmt.Errorf("openapiimport: options: publisher is required (an imported tool needs an owner)")
	}
	o.License = strings.TrimSpace(o.License)
	if o.License == "" {
		return fmt.Errorf("openapiimport: options: license is required (an imported tool must be attributable)")
	}
	if o.Version == "" {
		o.Version = strings.TrimSpace(spec.Info.Version)
	}
	if !versionPattern.MatchString(o.Version) {
		return fmt.Errorf("openapiimport: options: version %q must be pinned MAJOR.MINOR.PATCH (set Options.Version when the spec uses a non-semver number)", o.Version)
	}
	if o.Server == "" {
		o.Server = slugify(o.Publisher) + "-api"
	}
	if !slugPattern.MatchString(o.Server) {
		return fmt.Errorf("openapiimport: options: server %q must be a DNS-safe slug", o.Server)
	}
	if o.TenantContextKey == "" {
		o.TenantContextKey = defaultTenantKey
	}
	if o.TimeoutSeconds == 0 {
		o.TimeoutSeconds = defaultTimeout
	}
	if o.TimeoutSeconds < 1 || o.TimeoutSeconds > 86400 {
		return fmt.Errorf("openapiimport: options: timeoutSeconds must be 1-86400")
	}
	return nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "api"
	}
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}

// allowEntry is one parsed allow-list item: an operationId, "METHOD /path", or
// a bare path (every method on it).
type allowEntry struct {
	method string
	path   string
	opID   string
}

type allowSet struct {
	all     bool
	entries []allowEntry
}

// permits decides whether one operation is inside the audited surface.
func (a allowSet) permits(method, path, opID string) bool {
	if a.all {
		return true
	}
	for _, e := range a.entries {
		if e.opID != "" && opID != "" && e.opID == opID {
			return true
		}
		if e.path == "" || e.path != path {
			continue
		}
		if e.method == "" || strings.EqualFold(e.method, method) {
			return true
		}
	}
	return false
}

func (o Options) allowSet(spec *Spec) (allowSet, error) {
	if o.AllowAll {
		return allowSet{all: true}, nil
	}
	if len(o.Allow) == 0 {
		return allowSet{}, fmt.Errorf("openapiimport: an import must name the surface it exposes (Options.Allow); refusing to import a whole spec by default")
	}
	// Index the spec so a typo in the allow-list fails loudly instead of
	// silently importing nothing.
	paths := map[string]bool{}
	ops := map[string]bool{}
	for path, item := range spec.Paths {
		any := false
		for _, m := range item.methods() {
			if m.op == nil {
				continue
			}
			any = true
			if m.op.OperationID != "" {
				ops[m.op.OperationID] = true
			}
		}
		if any {
			paths[path] = true
		}
	}
	var out allowSet
	for _, raw := range o.Allow {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return allowSet{}, fmt.Errorf("openapiimport: options: empty allow-list entry")
		}
		fields := strings.Fields(entry)
		switch {
		case len(fields) == 2 && strings.HasPrefix(fields[1], "/") && paths[fields[1]]:
			if !operationExists(spec, strings.ToUpper(fields[0]), fields[1]) {
				return allowSet{}, fmt.Errorf("openapiimport: options: allow entry %q names a method the path does not declare", entry)
			}
			out.entries = append(out.entries, allowEntry{method: strings.ToUpper(fields[0]), path: fields[1]})
		case len(fields) == 1 && ops[entry]:
			out.entries = append(out.entries, allowEntry{opID: entry})
		case len(fields) == 1 && strings.HasPrefix(entry, "/") && paths[entry]:
			out.entries = append(out.entries, allowEntry{path: entry})
		default:
			return allowSet{}, fmt.Errorf("openapiimport: options: allow entry %q is not an operationId or 'METHOD /path' present in the spec", entry)
		}
	}
	return out, nil
}

func operationExists(spec *Spec, method, path string) bool {
	item, ok := spec.Paths[path]
	if !ok {
		return false
	}
	for _, m := range item.methods() {
		if strings.EqualFold(m.name, method) && m.op != nil {
			return true
		}
	}
	return false
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
