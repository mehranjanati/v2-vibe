package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkillFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestDefaultRegistryConfigs pins the per-role defaults to the model ids
// declared in backend/docs/architecture-roadmap.json.
func TestDefaultRegistryConfigs(t *testing.T) {
	r := NewRegistryWithDir(t.TempDir())

	planner, ok := r.Config(RolePlanner)
	if !ok {
		t.Fatal("planner role not registered")
	}
	if planner.Model != "@cf/meta/llama-3.3-70b-instruct-fp8-fast" {
		t.Errorf("planner model = %q", planner.Model)
	}
	if planner.SkillFile != "01_planner.md" {
		t.Errorf("planner skill file = %q", planner.SkillFile)
	}
	if planner.MaxTokens != 8192 || planner.Temperature != 0.2 {
		t.Errorf("planner params = %v/%v", planner.Temperature, planner.MaxTokens)
	}

	coder, ok := r.Config(RoleCoder)
	if !ok {
		t.Fatal("coder role not registered")
	}
	if coder.Model != "@cf/qwen/qwen2.5-coder-32b-instruct" {
		t.Errorf("coder model = %q", coder.Model)
	}
	if coder.SkillFile != "02_coder.md" {
		t.Errorf("coder skill file = %q", coder.SkillFile)
	}

	roles := r.Roles()
	if len(roles) != 4 || roles[0].Role != RoleCoordinator || roles[1].Role != RolePlanner || roles[2].Role != RoleCoder || roles[3].Role != RoleReviewer {
		t.Errorf("Roles() = %v; want coordinator, planner, coder, reviewer", roleNames(roles))
	}
	coord, ok := r.Config(RoleCoordinator)
	if !ok || coord.SkillFile != "00_coordinator.md" {
		t.Errorf("coordinator config = %+v, %v", coord, ok)
	}
	reviewer, ok := r.Config(RoleReviewer)
	if !ok || reviewer.SkillFile != "03_reviewer.md" {
		t.Errorf("reviewer config = %+v, %v", reviewer, ok)
	}
}

func roleNames(roles []RoleConfig) []string {
	out := make([]string, 0, len(roles))
	for _, rc := range roles {
		out = append(out, rc.Role)
	}
	return out
}

// TestLoadReadsSkillFiles verifies the happy path: both prompts load
// verbatim (trimmed of surrounding whitespace only).
func TestLoadReadsSkillFiles(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "00_coordinator.md", "COORDINATOR PROMPT delegate\n")
	writeSkillFile(t, dir, "01_planner.md", "PLANNER PROMPT thought_process\n")
	writeSkillFile(t, dir, "02_coder.md", "CODER PROMPT no fences\n")
	writeSkillFile(t, dir, "03_reviewer.md", "REVIEWER PROMPT approve\n")

	r := NewRegistryWithDir(dir)
	prompts, err := r.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prompts[RolePlanner] != "PLANNER PROMPT thought_process" {
		t.Errorf("planner prompt = %q", prompts[RolePlanner])
	}
	if prompts[RoleCoder] != "CODER PROMPT no fences" {
		t.Errorf("coder prompt = %q", prompts[RoleCoder])
	}

	p, err := r.LoadRole(RoleCoder)
	if err != nil {
		t.Fatalf("LoadRole: %v", err)
	}
	if p.Config.Model != "@cf/qwen/qwen2.5-coder-32b-instruct" || p.Text != "CODER PROMPT no fences" {
		t.Errorf("LoadRole = %+v", p)
	}
}

// TestLoadMissingSkillFileGivesClearError covers the acceptance rule:
// a missing skill file must surface a precise, actionable error.
func TestLoadMissingSkillFileGivesClearError(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "00_coordinator.md", "coord")
	writeSkillFile(t, dir, "01_planner.md", "planner prompt")
	writeSkillFile(t, dir, "03_reviewer.md", "review")

	r := NewRegistryWithDir(dir)
	_, err := r.Load()
	if err == nil {
		t.Fatal("Load succeeded with a missing coder prompt")
	}
	for _, want := range []string{"coder", "02_coder.md", dir, "SKILLS_DIR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

// TestLoadEmptySkillFile rejects whitespace-only prompt files.
func TestLoadEmptySkillFile(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "00_coordinator.md", "coord")
	writeSkillFile(t, dir, "01_planner.md", "planner prompt")
	writeSkillFile(t, dir, "02_coder.md", "   \n\t")
	writeSkillFile(t, dir, "03_reviewer.md", "review")

	r := NewRegistryWithDir(dir)
	_, err := r.Load()
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("err = %v, want an is-empty error", err)
	}
}

// TestLoadRoleUnknownRole guards the role key.
func TestLoadRoleUnknownRole(t *testing.T) {
	r := NewRegistryWithDir(t.TempDir())
	if _, err := r.LoadRole("unknown-role-xyz"); err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("err = %v, want unknown-role error", err)
	}
}

// TestEnvOverrides verifies SKILLS_DIR resolution and the per-role env
// overrides, including tolerance of invalid values.
func TestEnvOverrides(t *testing.T) {
	orig := envLookup
	env := map[string]string{
		"SKILLS_DIR":         "custom-skills",
		"PLANNER_MODEL":      "@cf/meta/custom-planner",
		"PLANNER_MAX_TOKENS": "bogus", // ignored, default stays
		"CODER_MAX_TOKENS":   "4096",
		"CODER_TEMPERATURE":  "0.3",
	}
	envLookup = func(k string) string { return env[k] }
	defer func() { envLookup = orig }()

	r := NewRegistry()
	if r.SkillsDir() != "custom-skills" {
		t.Errorf("skills dir = %q, want custom-skills", r.SkillsDir())
	}
	planner := r.Roles()[1]
	if planner.Model != "@cf/meta/custom-planner" {
		t.Errorf("planner model override not applied: %q", planner.Model)
	}
	if planner.MaxTokens != 8192 {
		t.Errorf("planner max tokens = %d, want default 8192 for a bogus override", planner.MaxTokens)
	}
	coder := r.Roles()[2]
	if coder.MaxTokens != 4096 || coder.Temperature != 0.3 {
		t.Errorf("coder overrides = %v/%v, want 0.3/4096", coder.Temperature, coder.MaxTokens)
	}
}

// TestResolveSkillsDirProbing walks the conventional locations the same
// way the binary does: from backend/ (or /app in the container), from a
// nested package dir during go test, and from the repo root.
func TestResolveSkillsDirProbing(t *testing.T) {
	orig := envLookup
	envLookup = func(string) string { return "" } // ignore SKILLS_DIR
	defer func() { envLookup = orig }()

	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(root, "backend", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backend", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(prev); err != nil {
			t.Logf("restore cwd: %v", err)
		}
	}()

	if err := os.Chdir(filepath.Join(root, "backend")); err != nil {
		t.Fatal(err)
	}
	if got := resolveSkillsDir(); got != DefaultSkillsDirName {
		t.Errorf("from backend/: got %q, want %q", got, DefaultSkillsDirName)
	}

	if err := os.Chdir(filepath.Join(root, "backend", "pkg")); err != nil {
		t.Fatal(err)
	}
	if got := resolveSkillsDir(); got != filepath.Join("..", DefaultSkillsDirName) {
		t.Errorf("from backend/pkg: got %q, want %q", got, "../skills")
	}

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if got := resolveSkillsDir(); got != filepath.Join("backend", DefaultSkillsDirName) {
		t.Errorf("from repo root: got %q, want %q", got, "backend/skills")
	}
}
