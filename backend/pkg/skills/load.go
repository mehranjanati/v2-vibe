package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads every registered skill prompt file from the skills dir and
// returns the verbatim system prompts keyed by role. It fails fast with
// a precise error when a file is missing or empty, so a broken skills
// directory surfaces at startup instead of producing prompt-less model
// calls later.
func (r Registry) Load() (map[string]string, error) {
	prompts := make(map[string]string, len(r.roles))
	for _, cfg := range r.Roles() {
		text, err := r.loadRole(cfg)
		if err != nil {
			return nil, err
		}
		prompts[cfg.Role] = text
	}
	return prompts, nil
}

// LoadRole loads a single role's skill prompt together with its config.
func (r Registry) LoadRole(role string) (Prompt, error) {
	cfg, ok := r.roles[role]
	if !ok {
		return Prompt{}, fmt.Errorf("skills: unknown role %q (registered: %s, %s, %s, %s)", role, RoleCoordinator, RolePlanner, RoleCoder, RoleReviewer)
	}
	text, err := r.loadRole(cfg)
	if err != nil {
		return Prompt{}, err
	}
	return Prompt{Config: cfg, Text: text}, nil
}

// loadRole reads one skill file. The text is trimmed of surrounding
// whitespace only; the content itself is used verbatim.
func (r Registry) loadRole(cfg RoleConfig) (string, error) {
	path := filepath.Join(r.skillsDir, cfg.SkillFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		all := defaultRoles()
		return "", fmt.Errorf(
			"skills: %s prompt %q not found in %q: %w (set SKILLS_DIR to the folder containing %s, %s, %s and %s)",
			cfg.Role, cfg.SkillFile, r.skillsDir, err,
			all[RoleCoordinator].SkillFile, all[RolePlanner].SkillFile, all[RoleCoder].SkillFile, all[RoleReviewer].SkillFile)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", fmt.Errorf("skills: %s prompt %q in %q is empty", cfg.Role, cfg.SkillFile, r.skillsDir)
	}
	return text, nil
}

// resolveSkillsDir finds the directory that holds the skill prompt
// files: SKILLS_DIR wins when set; otherwise conventional locations
// relative to the process working directory are probed. This covers
// `go run .` from backend/ ("skills"), `go test` from nested package
// dirs ("../skills", "../../skills") and the container ("/app" ->
// "skills"). When nothing exists, "skills" is returned so Load reports
// the precise failure.
func resolveSkillsDir() string {
	if dir := strings.TrimSpace(envLookup("SKILLS_DIR")); dir != "" {
		return dir
	}
	candidates := []string{
		DefaultSkillsDirName,
		filepath.Join("..", DefaultSkillsDirName),
		filepath.Join("..", "..", DefaultSkillsDirName),
		filepath.Join("backend", DefaultSkillsDirName),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return DefaultSkillsDirName
}
