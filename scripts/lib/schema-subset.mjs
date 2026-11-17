// Shared JSON-Schema subset + canonical serialization used by the docs gates
// (`scripts/validate-manifests.mjs` for P7.1 and `scripts/validate-system-ir.mjs`
// for P7.2). Dependency-free on purpose: `ajv` is only a transitive dependency
// here, so a docs gate must not rely on it.
//
// Supported keywords: type, enum, const, pattern, minLength, maxLength,
// minimum, maximum, required, properties, additionalProperties, items,
// minItems, uniqueItems, allOf, oneOf, and local `$ref` into `$defs`.
// Anything beyond this subset must be added here in the same change that uses
// it (documented in docs/architecture/manifest-schemas.md).

export function createSubsetValidator(errors) {
  const fail = (msg) => errors.push(msg);

  function resolveLocalRef(schema, ref) {
    if (!ref.startsWith('#/')) return null;
    let node = schema;
    for (const rawPart of ref.slice(2).split('/')) {
      const part = rawPart.replace(/~1/g, '/').replace(/~0/g, '~');
      if (node === null || typeof node !== 'object' || !(part in node)) return null;
      node = node[part];
    }
    return node;
  }

  function typeOf(value) {
    if (value === null) return 'null';
    if (Array.isArray(value)) return 'array';
    if (Number.isInteger(value)) return 'integer';
    return typeof value;
  }

  function matchesType(value, expected) {
    const actual = typeOf(value);
    if (expected === 'number') return actual === 'number' || actual === 'integer';
    if (expected === 'integer') return actual === 'integer';
    return actual === expected;
  }

  function validateNode(value, schema, rootSchema, at, pathLabel) {
    if (schema === true) return;
    if (schema === false) {
      fail(`${pathLabel}: ${at} is not allowed`);
      return;
    }

    if (schema.$ref) {
      const target = resolveLocalRef(rootSchema, schema.$ref);
      if (!target) {
        fail(`${pathLabel}: ${at} uses unresolvable $ref '${schema.$ref}'`);
        return;
      }
      validateNode(value, target, rootSchema, at, pathLabel);
      return;
    }

    if (Array.isArray(schema.allOf)) {
      for (const branch of schema.allOf) validateNode(value, branch, rootSchema, at, pathLabel);
    }

    if (schema.type) {
      const expected = Array.isArray(schema.type) ? schema.type : [schema.type];
      if (!expected.some((t) => matchesType(value, t))) {
        fail(`${pathLabel}: ${at} must be ${expected.join('|')}, got ${typeOf(value)}`);
        return;
      }
    }

    if (schema.const !== undefined && value !== schema.const) {
      fail(`${pathLabel}: ${at} must equal ${JSON.stringify(schema.const)}`);
    }
    if (schema.enum && !schema.enum.includes(value)) {
      fail(`${pathLabel}: ${at} must be one of ${schema.enum.join('|')}, got ${JSON.stringify(value)}`);
    }

    if (typeof value === 'string') {
      if (schema.minLength !== undefined && value.length < schema.minLength) {
        fail(`${pathLabel}: ${at} shorter than ${schema.minLength}`);
      }
      if (schema.maxLength !== undefined && value.length > schema.maxLength) {
        fail(`${pathLabel}: ${at} longer than ${schema.maxLength}`);
      }
      if (schema.pattern && !new RegExp(schema.pattern, 'u').test(value)) {
        fail(`${pathLabel}: ${at} does not match ${schema.pattern} (got ${JSON.stringify(value)})`);
      }
    }

    if (typeof value === 'number') {
      if (schema.minimum !== undefined && value < schema.minimum) {
        fail(`${pathLabel}: ${at} below minimum ${schema.minimum}`);
      }
      if (schema.maximum !== undefined && value > schema.maximum) {
        fail(`${pathLabel}: ${at} above maximum ${schema.maximum}`);
      }
    }

    if (Array.isArray(value)) {
      if (schema.minItems !== undefined && value.length < schema.minItems) {
        fail(`${pathLabel}: ${at} needs at least ${schema.minItems} item(s)`);
      }
      if (schema.uniqueItems) {
        const seen = new Set();
        for (const item of value) {
          const key = canonical(item);
          if (seen.has(key)) fail(`${pathLabel}: ${at} has duplicate item ${key}`);
          seen.add(key);
        }
      }
      if (schema.items) {
        value.forEach((item, i) => validateNode(item, schema.items, rootSchema, `${at}[${i}]`, pathLabel));
      }
    }

    if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
      const props = schema.properties || {};
      for (const key of schema.required || []) {
        if (!(key in value)) fail(`${pathLabel}: ${at} is missing required property '${key}'`);
      }
      for (const [key, child] of Object.entries(value)) {
        if (key in props) {
          validateNode(child, props[key], rootSchema, `${at}.${key}`, pathLabel);
        } else if (schema.additionalProperties === false) {
          const allowed = Object.keys(props).join(', ') || '(none)';
          fail(`${pathLabel}: ${at} has unexpected property '${key}' (allowed: ${allowed})`);
        } else if (schema.additionalProperties && typeof schema.additionalProperties === 'object') {
          validateNode(child, schema.additionalProperties, rootSchema, `${at}.${key}`, pathLabel);
        }
      }
    }

    if (Array.isArray(schema.oneOf)) {
      const matched = schema.oneOf.filter((candidate) => {
        const before = errors.length;
        validateNode(value, candidate, rootSchema, at, pathLabel);
        const ok = errors.length === before;
        if (!ok) errors.length = before; // oneOf branches are probes
        return ok;
      });
      if (matched.length !== 1) {
        fail(`${pathLabel}: ${at} must match exactly one oneOf branch (matched ${matched.length})`);
      }
    }
  }

  // --- deterministic serialization ---------------------------------------------
  function canonical(value) {
    if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
    if (value && typeof value === 'object') {
      const keys = Object.keys(value).sort();
      return `{${keys.map((k) => `${JSON.stringify(k)}:${canonical(value[k])}`).join(',')}}`;
    }
    return JSON.stringify(value);
  }

  function serializeManifest(value) {
    // Canonical form: keys sorted recursively, 2-space indent, trailing newline.
    const sort = (node) => {
      if (Array.isArray(node)) return node.map(sort);
      if (node && typeof node === 'object') {
        return Object.fromEntries(Object.keys(node).sort().map((k) => [k, sort(node[k])]));
      }
      return node;
    };
    return `${JSON.stringify(sort(value), null, 2)}\n`;
  }

  return { validate: validateNode, canonical, serialize: serializeManifest };
}
