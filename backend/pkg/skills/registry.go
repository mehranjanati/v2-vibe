// Package skills provides the per-role model registry and the loader for
// the role system prompts ("skills") stored in backend/skills/.
//
// The dual-model pipeline maps every role (planner, coder) onto one
// Cloudflare Workers AI model behind the AI Gateway and one markdown
// prompt file that is injected verbatim as the system message of every
// call made for that role. Defaults mirror
// backend/docs/architecture-roadmap.json and can be overridden per
// environment (SKILLS_DIR, PLANNER_MODEL, CODER_MODEL, ...).
package skills

import (
	"os"
	"strconv"
	"strings"
)

// Role identifiers used across the dual-model pipeline.
const (
	RoleCoordinator = "coordinator"
	RolePlanner     = "planner"
	RoleCoder       = "coder"
	RoleReviewer    = "reviewer"
)

// DefaultSkillsDirName is the directory (relative to the process working
// directory, or resolved via SKILLS_DIR) that holds the skill prompt
// files. The production container runs from /app, so the directory is
// /app/skills there (see backend/Dockerfile).
const DefaultSkillsDirName = "skills"

// RoleConfig describes how one pipeline role maps onto a Workers AI
// model (via the AI Gateway) and which skill prompt drives it. The
// values feed straight into llm.ChatRequest.
type RoleConfig struct {
	Role        string  // RolePlanner or RoleCoder
	Model       string  // Workers AI model id, e.g. @cf/meta/llama-3.3-70b-instruct-fp8-fast
	SkillFile   string  // prompt file name inside the skills dir
	Temperature float64 // generation temperature
	MaxTokens   int     // generation max_tokens
}

// Prompt is a loaded skill: the resolved role config plus the verbatim
// system prompt text read from the skill file.
type Prompt struct {
	Config RoleConfig
	Text   string
}

// envLookup is a thin indirection so tests can stub it (same pattern
// as pkg/llm).
var envLookup = os.Getenv

// defaultRoles mirrors the "models" section of
// backend/docs/architecture-roadmap.json (v0.2.0).
func defaultRoles() map[string]RoleConfig {
	return map[string]RoleConfig{
		RoleCoordinator: {
			Role:        RoleCoordinator,
			Model:       "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
			SkillFile:   "00_coordinator.md",
			Temperature: 0.2,
			MaxTokens:   8192,
		},
		RolePlanner: {
			Role:        RolePlanner,
			Model:       "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
			SkillFile:   "01_planner.md",
			Temperature: 0.2,
			MaxTokens:   8192,
		},
		RoleCoder: {
			Role:        RoleCoder,
			Model:       "@cf/qwen/qwen2.5-coder-32b-instruct",
			SkillFile:   "02_coder.md",
			Temperature: 0.1,
			MaxTokens:   8192,
		},
		RoleReviewer: {
			Role:        RoleReviewer,
			Model:       "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
			SkillFile:   "03_reviewer.md",
			Temperature: 0.1,
			MaxTokens:   4096,
		},
	}
}

// Registry binds the pipeline roles to their model configs and to the
// directory that holds the skill prompt files. It is immutable once
// built and safe for concurrent use.
type Registry struct {
	skillsDir string
	roles     map[string]RoleConfig
}

// NewRegistry resolves the skills directory (SKILLS_DIR override, then
// conventional locations) and applies the per-role environment
// overrides on top of the defaults.
func NewRegistry() Registry {
	return NewRegistryWithDir(resolveSkillsDir())
}

// NewRegistryWithDir builds a registry pinned to an explicit directory.
func NewRegistryWithDir(dir string) Registry {
	roles := make(map[string]RoleConfig, 4)
	for role, cfg := range defaultRoles() {
		roles[role] = cfg.withEnvOverrides()
	}
	return Registry{skillsDir: dir, roles: roles}
}

// SkillsDir returns the directory the registry loads prompts from.
func (r Registry) SkillsDir() string { return r.skillsDir }

// Roles returns the registered configs in pipeline order
// (coordinator first, then planner, coder, reviewer).
func (r Registry) Roles() []RoleConfig {
	return []RoleConfig{r.roles[RoleCoordinator], r.roles[RolePlanner], r.roles[RoleCoder], r.roles[RoleReviewer]}
}

// Config returns the config registered for a role.
func (r Registry) Config(role string) (RoleConfig, bool) {
	cfg, ok := r.roles[role]
	return cfg, ok
}

// withEnvOverrides applies the optional PLANNER_/CODER_ prefixed
// environment variables on top of the defaults. Invalid values are
// ignored (the default stays in effect) so a typo never hard-fails the
// registry; SKILLS_DIR is handled by resolveSkillsDir in load.go.
func (c RoleConfig) withEnvOverrides() RoleConfig {
	prefix := strings.ToUpper(c.Role) + "_"
	if v := strings.TrimSpace(envLookup(prefix + "MODEL")); v != "" {
		c.Model = v
	}
	if v := strings.TrimSpace(envLookup(prefix + "MAX_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxTokens = n
		}
	}
	if v := strings.TrimSpace(envLookup(prefix + "TEMPERATURE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 2 {
			c.Temperature = f
		}
	}
	return c
}
