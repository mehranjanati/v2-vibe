package openapiimport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"backend/pkg/mcp"
)

// Manifest is one Tool Manifest as a key-agnostic map. A map (not a struct) is
// deliberate: Go sorts map keys when marshaling, which is exactly the canonical
// form the docs validator enforces (sorted keys, 2-space indent, trailing
// newline).
type Manifest map[string]interface{}

// Imported pairs a manifest with the provenance the importer used to build it:
// the Gate needs a registry server key, and reviewers need the operation.
type Imported struct {
	Manifest    Manifest
	Server      string
	OperationID string
	Method      string
	Path        string
}

// Name returns metadata.name.
func (i Imported) Name() string { return stringField(i.Manifest, "metadata", "name") }

// Version returns metadata.version.
func (i Imported) Version() string { return stringField(i.Manifest, "metadata", "version") }

// Ref returns the pinned content address `tool/<name>@<x.y.z>`.
func (i Imported) Ref() string { return "tool/" + i.Name() + "@" + i.Version() }

// Bytes renders the canonical manifest: sorted keys, 2-space indent, trailing
// newline — byte-identical to scripts/lib/schema-subset.mjs serialize().
func (i Imported) Bytes() ([]byte, error) {
	raw, err := json.MarshalIndent(map[string]interface{}(i.Manifest), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("openapiimport: marshal %s: %w", i.Ref(), err)
	}
	return append(raw, '\n'), nil
}

// Policy maps the manifest onto the Gate's policy vocabulary, so an imported
// tool is enforced by exactly the same checks as any other tool.
func (i Imported) Policy() (mcp.ToolPolicy, error) {
	spec, ok := i.Manifest["spec"].(map[string]interface{})
	if !ok {
		return mcp.ToolPolicy{}, fmt.Errorf("openapiimport: %s: manifest has no spec", i.Ref())
	}
	p := mcp.ToolPolicy{
		Server:              i.Server,
		Tool:                i.Name(),
		ToolManifestVersion: i.Version(),
		SideEffect:          mcp.SideEffect(strIn(spec, "sideEffect")),
	}
	if v, ok := spec["timeoutSeconds"].(int); ok {
		p.TimeoutSeconds = v
	}
	for _, raw := range arrIn(spec, "permissions") {
		if s, ok := raw.(string); ok {
			p.Permissions = append(p.Permissions, s)
		}
	}
	for _, raw := range arrIn(spec, "approvals") {
		if s, ok := raw.(string); ok {
			p.Approvals = append(p.Approvals, mcp.ApprovalClass(s))
		}
	}
	for _, raw := range arrIn(spec, "secrets") {
		if s, ok := raw.(string); ok {
			p.Secrets = append(p.Secrets, s)
		}
	}
	if idem, ok := spec["idempotency"].(map[string]interface{}); ok {
		p.IdempotencyKey = strIn(idem, "key")
	}
	for _, a := range p.Approvals {
		if a == mcp.ApprovalPayment || a == mcp.ApprovalHumanReview {
			p.RequiresHuman = true
		}
	}
	if err := p.Valid(); err != nil {
		return mcp.ToolPolicy{}, fmt.Errorf("openapiimport: %s: %w", i.Ref(), err)
	}
	return p, nil
}

var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	versionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	permissionPattern = regexp.MustCompile(`^[a-z0-9]+(:[a-z0-9-]+)+$`)
	vaultPattern      = regexp.MustCompile(`^vault:[a-z0-9][a-z0-9/_.-]*$`)
)

var approvalVocabulary = map[string]bool{
	string(mcp.ApprovalExternalMessage): true,
	string(mcp.ApprovalExternalWrite):   true,
	string(mcp.ApprovalPayment):         true,
	string(mcp.ApprovalHumanReview):     true,
}

// Validate checks the manifest against the Tool Manifest contract
// (docs/architecture/manifest-schemas/tool.schema.json) before the importer
// returns it. The subset here is what the importer can produce; keeping the
// check next to the builder means a bad manifest fails at import, not at review
// or at call time.
func (i Imported) Validate() error {
	if apiVersion := stringField(i.Manifest, "apiVersion"); apiVersion != "vibesdk/v1" {
		return fmt.Errorf("openapiimport: %s: apiVersion %q must be vibesdk/v1", i.Ref(), apiVersion)
	}
	if kind := stringField(i.Manifest, "kind"); kind != "Tool" {
		return fmt.Errorf("openapiimport: %s: kind %q must be Tool", i.Ref(), kind)
	}
	meta, ok := i.Manifest["metadata"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("openapiimport: %s: metadata is required", i.Ref())
	}
	if name := strIn(meta, "name"); !slugPattern.MatchString(name) || len(name) > 63 {
		return fmt.Errorf("openapiimport: %s: metadata.name %q must be a slug of at most 63 characters", i.Ref(), name)
	}
	if v := strIn(meta, "version"); !versionPattern.MatchString(v) {
		return fmt.Errorf("openapiimport: %s: metadata.version %q must be pinned MAJOR.MINOR.PATCH", i.Ref(), v)
	}
	if pub := strIn(meta, "publisher"); pub == "" || len(pub) > 120 {
		return fmt.Errorf("openapiimport: %s: metadata.publisher is required (max 120 characters)", i.Ref())
	}
	if lic := strIn(meta, "license"); lic == "" || len(lic) > 80 {
		return fmt.Errorf("openapiimport: %s: metadata.license is required (max 80 characters)", i.Ref())
	}
	if desc := strIn(meta, "description"); len(desc) > 500 {
		return fmt.Errorf("openapiimport: %s: metadata.description exceeds 500 characters", i.Ref())
	}
	spec, ok := i.Manifest["spec"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("openapiimport: %s: spec is required", i.Ref())
	}
	if d := strIn(spec, "description"); d == "" || len(d) > 500 {
		return fmt.Errorf("openapiimport: %s: spec.description must be 1-500 characters", i.Ref())
	}
	if _, ok := spec["inputSchema"].(map[string]interface{}); !ok {
		return fmt.Errorf("openapiimport: %s: spec.inputSchema must be an object", i.Ref())
	}
	if _, ok := spec["outputSchema"].(map[string]interface{}); !ok {
		return fmt.Errorf("openapiimport: %s: spec.outputSchema must be an object", i.Ref())
	}
	perms := arrIn(spec, "permissions")
	if len(perms) == 0 {
		return fmt.Errorf("openapiimport: %s: spec.permissions needs at least one entry", i.Ref())
	}
	seenPerm := map[string]bool{}
	for _, raw := range perms {
		s, ok := raw.(string)
		if !ok || !permissionPattern.MatchString(s) {
			return fmt.Errorf("openapiimport: %s: permission %v must look like resource:action", i.Ref(), raw)
		}
		if seenPerm[s] {
			return fmt.Errorf("openapiimport: %s: duplicate permission %q", i.Ref(), s)
		}
		seenPerm[s] = true
	}
	return i.validateAuthority(spec)
}

// validateAuthority checks the fields that decide what a call may do.
func (i Imported) validateAuthority(spec map[string]interface{}) error {
	if se := strIn(spec, "sideEffect"); se != "safe" && se != "external" && se != "irreversible" {
		return fmt.Errorf("openapiimport: %s: sideEffect %q must be safe, external or irreversible", i.Ref(), se)
	}
	approvals := arrIn(spec, "approvals")
	for _, raw := range approvals {
		s, ok := raw.(string)
		if !ok || !approvalVocabulary[s] {
			return fmt.Errorf("openapiimport: %s: approval %v is not a Tool Manifest class", i.Ref(), raw)
		}
	}
	if strIn(spec, "sideEffect") == "irreversible" && len(approvals) == 0 {
		return fmt.Errorf("openapiimport: %s: irreversible tools require at least one approval class", i.Ref())
	}
	tenant, ok := spec["tenant"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("openapiimport: %s: spec.tenant is required", i.Ref())
	}
	if strIn(tenant, "isolation") != "required" {
		return fmt.Errorf("openapiimport: %s: spec.tenant.isolation must be \"required\"", i.Ref())
	}
	if key := strIn(tenant, "contextKey"); key == "" || len(key) > 80 {
		return fmt.Errorf("openapiimport: %s: spec.tenant.contextKey must be 1-80 characters", i.Ref())
	}
	seenSecret := map[string]bool{}
	for _, raw := range arrIn(spec, "secrets") {
		s, ok := raw.(string)
		if !ok || !vaultPattern.MatchString(s) {
			return fmt.Errorf("openapiimport: %s: secret %v must be a vault: reference, never a literal", i.Ref(), raw)
		}
		if seenSecret[s] {
			return fmt.Errorf("openapiimport: %s: duplicate secret %q", i.Ref(), s)
		}
		seenSecret[s] = true
	}
	if idem, ok := spec["idempotency"].(map[string]interface{}); ok {
		key := strIn(idem, "key")
		if key == "" || len(key) > 200 {
			return fmt.Errorf("openapiimport: %s: idempotency.key must be 1-200 characters", i.Ref())
		}
		if ttl, ok := idem["ttlSeconds"].(int); ok && (ttl < 0 || ttl > 2592000) {
			return fmt.Errorf("openapiimport: %s: idempotency.ttlSeconds must be 0-2592000", i.Ref())
		}
	}
	if t, ok := spec["timeoutSeconds"].(int); ok && (t < 1 || t > 86400) {
		return fmt.Errorf("openapiimport: %s: timeoutSeconds must be 1-86400", i.Ref())
	}
	adapter, ok := spec["adapter"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("openapiimport: %s: spec.adapter is required", i.Ref())
	}
	switch strIn(adapter, "type") {
	case "native", "function", "http", "mcp":
	default:
		return fmt.Errorf("openapiimport: %s: adapter.type %q is not a Tool Manifest adapter", i.Ref(), strIn(adapter, "type"))
	}
	return nil
}

func strIn(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

func arrIn(m map[string]interface{}, key string) []interface{} {
	out, _ := m[key].([]interface{})
	return out
}

func stringField(m map[string]interface{}, path ...string) string {
	var current interface{} = m
	for _, key := range path {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = obj[key]
	}
	s, _ := current.(string)
	return s
}

// canonicalEqual reports whether two manifests serialize identically, which is
// how Merge detects "same capability, same content" without comparing maps.
func canonicalEqual(a, b Manifest) bool {
	ra, errA := json.Marshal(map[string]interface{}(a))
	rb, errB := json.Marshal(map[string]interface{}(b))
	return errA == nil && errB == nil && bytes.Equal(ra, rb)
}
