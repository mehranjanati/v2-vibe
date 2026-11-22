package openapiimport

import (
	"fmt"
	"strings"
)

// vaultRef names the vault path a credential resolves from. The reference
// shape is the manifest contract's: `vault:<publisher>/<slug>`.
func vaultRef(publisher, name string) string {
	return "vault:" + slugify(publisher) + "/" + slugify(name)
}

// isCredentialParam reports whether a parameter carries a secret. Such a
// parameter is never imported: the value comes from the vault at call time, so
// a model can neither see it nor supply it.
func isCredentialParam(p Parameter) bool {
	if p.In == "cookie" {
		return true
	}
	return p.In == "header" && credentialHeaderNames[strings.ToLower(p.Name)]
}

// jsonSchemaOf returns the JSON schema of the first JSON media type (a plain
// `application/json` wins over `application/*+json`).
func jsonSchemaOf(content map[string]MediaType) map[string]interface{} {
	if mt, ok := content["application/json"]; ok && mt.Schema != nil {
		return mt.Schema
	}
	for _, key := range sortedKeys(content) {
		if strings.Contains(strings.ToLower(key), "json") && content[key].Schema != nil {
			return content[key].Schema
		}
	}
	return nil
}

// inlineBody flattens an object body into the input schema so the model sees
// real fields instead of an opaque `body`. A body that is not a plain object
// (array, $ref-only, additionalProperties) stays a single `body` argument:
// guessing would lose structure.
func inlineBody(schema map[string]interface{}, add func(string, map[string]interface{}, bool) error) (bool, error) {
	if schema == nil {
		return false, nil
	}
	if t, ok := schema["type"].(string); !ok || t != "object" {
		return false, nil
	}
	if _, hasRef := schema["$ref"]; hasRef {
		return false, nil
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok || len(props) == 0 {
		return false, nil
	}
	required := map[string]bool{}
	if raw, ok := schema["required"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				required[s] = true
			}
		}
	}
	for _, field := range sortedKeys(props) {
		sub, _ := props[field].(map[string]interface{})
		if err := add(field, sub, required[field]); err != nil {
			return false, err
		}
	}
	return true, nil
}

// effectiveSecurity returns the security schemes an operation actually uses:
// its own requirement when declared (an empty array means "none"), otherwise
// the document-level requirement. A requirement naming an undefined scheme is
// an error — silently dropping it would import an unauthenticated tool.
func effectiveSecurity(spec *Spec, op *Operation) ([]string, error) {
	requirements := spec.Security
	if op.Security != nil {
		requirements = op.Security
	}
	seen := map[string]bool{}
	for _, requirement := range requirements {
		for _, schemeName := range sortedKeys(requirement) {
			scheme, ok := spec.Components.SecuritySchemes[schemeName]
			if !ok {
				return nil, fmt.Errorf("security requirement names undefined scheme %q", schemeName)
			}
			switch scheme.Type {
			case "apiKey", "http", "oauth2", "openIdConnect", "mutualTLS":
				seen[schemeName] = true
			default:
				return nil, fmt.Errorf("security scheme %q of type %q is not supported; declare it as apiKey/http/oauth2/openIdConnect or exclude the operation", schemeName, scheme.Type)
			}
		}
	}
	return sortedKeys(seen), nil
}

// outputSchema takes the first 2xx JSON schema the response declares. An
// undocumented response becomes a permissive object: the schema requires an
// output shape, and inventing fields would be a lie.
func outputSchema(op *Operation) map[string]interface{} {
	for _, status := range sortedKeys(op.Responses) {
		if !strings.HasPrefix(status, "2") {
			continue
		}
		if schema := jsonSchemaOf(op.Responses[status].Content); schema != nil {
			return schema
		}
	}
	return map[string]interface{}{"type": "object"}
}

// dedupe preserves order and drops repeats.
func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sortedAny renders a string set as a sorted []interface{} for the manifest.
func sortedAny(set map[string]bool) []interface{} {
	keys := sortedKeys(set)
	out := make([]interface{}, 0, len(keys))
	for _, k := range keys {
		out = append(out, k)
	}
	return out
}
