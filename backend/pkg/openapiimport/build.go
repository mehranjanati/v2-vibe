package openapiimport

import (
	"fmt"
	"sort"
	"strings"
)

// build turns one operation into a manifest. Every decision here is
// conservative: an unknown construct becomes the narrower choice (a body
// object instead of flattened fields, an approval instead of none, a vault
// reference instead of a literal).
func build(spec *Spec, path, method string, op *Operation, opts Options) (Imported, error) {
	name := toolName(op, method, path)
	if !slugPattern.MatchString(name) || len(name) > 63 {
		return Imported{}, fmt.Errorf("openapiimport: %s %s: derived tool name %q is not a valid slug", strings.ToUpper(method), path, name)
	}
	description := describe(op, method, path)
	side, approvals := authorityFor(method)

	props := map[string]interface{}{}
	required := []string{}
	addProp := func(field string, schema map[string]interface{}, isRequired bool) error {
		if _, exists := props[field]; exists {
			return fmt.Errorf("openapiimport: %s %s: field %q is declared twice (parameter/body collision); rename it in the spec", strings.ToUpper(method), path, field)
		}
		if schema == nil {
			schema = map[string]interface{}{"type": "string"}
		}
		props[field] = schema
		if isRequired {
			required = append(required, field)
		}
		return nil
	}

	secrets := map[string]bool{}
	for _, p := range op.Parameters {
		if isCredentialParam(p) {
			// Never imported: a credential is a vault reference, not an
			// argument a model may set.
			secrets[vaultRef(opts.Publisher, p.Name)] = true
			continue
		}
		switch p.In {
		case "path":
			if err := addProp(p.Name, p.Schema, true); err != nil {
				return Imported{}, err
			}
		case "query", "header":
			if err := addProp(p.Name, p.Schema, p.Required); err != nil {
				return Imported{}, err
			}
		case "cookie":
			// Cookie parameters are ambient credentials; they are
			// never model-supplied arguments.
			secrets[vaultRef(opts.Publisher, p.Name)] = true
		default:
			return Imported{}, fmt.Errorf("openapiimport: %s %s: parameter %q has unsupported location %q", strings.ToUpper(method), path, p.Name, p.In)
		}
	}

	if op.RequestBody != nil {
		if schema := jsonSchemaOf(op.RequestBody.Content); schema != nil {
			flattened, err := inlineBody(schema, addProp)
			if err != nil {
				return Imported{}, fmt.Errorf("openapiimport: %s %s: %w", strings.ToUpper(method), path, err)
			}
			if !flattened {
				if err := addProp("body", schema, op.RequestBody.Required); err != nil {
					return Imported{}, err
				}
			}
		}
	}

	schemes, err := effectiveSecurity(spec, op)
	if err != nil {
		return Imported{}, fmt.Errorf("openapiimport: %s %s: %w", strings.ToUpper(method), path, err)
	}
	for _, scheme := range schemes {
		secrets[vaultRef(opts.Publisher, scheme)] = true
	}

	inputSchema := map[string]interface{}{"type": "object"}
	if len(props) > 0 {
		inputSchema["properties"] = props
	}
	if len(required) > 0 {
		sort.Strings(required)
		required = dedupe(required)
		inputSchema["required"] = required
	}

	metadata := map[string]interface{}{
		"name":        name,
		"version":     opts.Version,
		"publisher":   opts.Publisher,
		"license":     opts.License,
		"description": description,
	}
	specBody := map[string]interface{}{
		"adapter":        map[string]interface{}{"type": "http", "toolName": strings.ToUpper(method) + " " + path},
		"approvals":      approvals,
		"description":    description,
		"inputSchema":    inputSchema,
		"outputSchema":   outputSchema(op),
		"permissions":    []interface{}{permission(opts, path, method)},
		"secrets":        sortedAny(secrets),
		"sideEffect":     side,
		"tenant":         map[string]interface{}{"isolation": "required", "contextKey": opts.TenantContextKey},
		"timeoutSeconds": opts.TimeoutSeconds,
	}
	if idem := idempotencyFor(op, method); idem != nil {
		specBody["idempotency"] = idem
	}
	if opts.Owner != "" {
		provenance := map[string]interface{}{"repo": opts.Owner}
		if opts.Commit != "" {
			provenance["commit"] = opts.Commit
		}
		specBody["provenance"] = provenance
	}

	return Imported{
		Manifest: Manifest{
			"apiVersion": "vibesdk/v1",
			"kind":       "Tool",
			"metadata":   metadata,
			"spec":       specBody,
		},
		Server:      opts.Server,
		OperationID: op.OperationID,
		Method:      strings.ToUpper(method),
		Path:        path,
	}, nil
}

// toolName derives the manifest name: the operationId when the spec has one
// (slugs are preserved), otherwise a stable method+path slug.
func toolName(op *Operation, method, path string) string {
	if op.OperationID != "" {
		if id := slugify(op.OperationID); slugPattern.MatchString(id) {
			if len(id) > 63 {
				id = strings.Trim(id[:63], "-")
			}
			return id
		}
	}
	parts := []string{strings.ToLower(method)}
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			parts = append(parts, "by", slugify(seg[1:len(seg)-1]))
			continue
		}
		parts = append(parts, slugify(seg))
	}
	name := strings.Trim(nonSlugChars.ReplaceAllString(strings.Join(parts, "-"), "-"), "-")
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	return name
}

// describe builds a one-paragraph description (summary, then description, then
// the signature) capped at the 500-character manifest limit.
func describe(op *Operation, method, path string) string {
	for _, candidate := range []string{op.Summary, op.Description} {
		if d := strings.Join(strings.Fields(candidate), " "); d != "" {
			if len(d) > 500 {
				d = strings.TrimSpace(d[:497]) + "..."
			}
			return d
		}
	}
	return strings.ToUpper(method) + " " + path
}

// authorityFor maps the HTTP method onto the Tool Manifest authority classes.
// DELETE is irreversible (there is no undo), so it always carries a
// human-review approval; writes carry external-write.
func authorityFor(method string) (string, []interface{}) {
	switch strings.ToLower(method) {
	case "get", "head", "options":
		return "safe", []interface{}{}
	case "post", "put", "patch":
		return "external", []interface{}{"external-write"}
	case "delete":
		return "irreversible", []interface{}{"human-review"}
	default:
		return "external", []interface{}{"external-write"}
	}
}

// permission derives the platform permission `resource:action` from the path
// and method. The resource must be lowercase alphanumerics (the Tool Manifest
// pattern forbids hyphens in the first segment), so the first static path
// segment is normalised.
func permission(opts Options, path, method string) string {
	resource := ""
	if opts.ResourceOverride != "" {
		resource = slugify(opts.ResourceOverride)
	} else {
		for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
			if seg == "" || strings.HasPrefix(seg, "{") {
				continue
			}
			if candidate := slugify(seg); isLowerAlnum(candidate) {
				resource = candidate
				break
			}
		}
	}
	resource = strings.Trim(nonSlugChars.ReplaceAllString(resource, ""), "-")
	if resource == "" {
		resource = "api"
	}
	action := "read"
	switch strings.ToLower(method) {
	case "post":
		action = "create"
	case "put", "patch":
		action = "update"
	case "delete":
		action = "delete"
	}
	return resource + ":" + action
}

func isLowerAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// idempotencyFor picks the argument field that deduplicates a mutating call:
// the resource path parameter when the operation addresses one resource, or an
// explicit idempotency field in the body.
func idempotencyFor(op *Operation, method string) map[string]interface{} {
	switch strings.ToLower(method) {
	case "put", "patch", "delete":
		for _, p := range op.Parameters {
			if p.In == "path" {
				return map[string]interface{}{"key": p.Name, "ttlSeconds": idempotencyTTL}
			}
		}
	case "post":
		if op.RequestBody != nil {
			if schema := jsonSchemaOf(op.RequestBody.Content); schema != nil {
				if fields, ok := schema["properties"].(map[string]interface{}); ok {
					for _, candidate := range []string{"idempotencyKey", "idempotency_key", "requestId", "request_id"} {
						if _, present := fields[candidate]; present {
							return map[string]interface{}{"key": candidate, "ttlSeconds": idempotencyTTL}
						}
					}
				}
			}
		}
	}
	return nil
}
