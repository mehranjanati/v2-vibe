package engine

import (
	"testing"

	"backend/pkg/skills"
)

func teamTestPrompts() teamSkillPrompts {
	return teamSkillPrompts{
		coordinator: "coordinate",
		coder:       "code",
		reviewer:    "review",
	}
}

func TestResolveTeamPrompts(t *testing.T) {
	got, err := resolveTeamPrompts(func(role string) (skills.Prompt, error) {
		return skills.Prompt{Text: role + "-prompt"}, nil
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.coordinator != "coordinator-prompt" || got.coder != "coder-prompt" || got.reviewer != "reviewer-prompt" {
		t.Fatalf("unexpected prompts: %+v", got)
	}
}

func TestResolveTeamPromptsRejectsEmpty(t *testing.T) {
	if _, err := resolveTeamPrompts(func(role string) (skills.Prompt, error) {
		if role == skills.RoleReviewer {
			return skills.Prompt{Text: "  "}, nil
		}
		return skills.Prompt{Text: "ok"}, nil
	}); err == nil {
		t.Fatal("expected error for empty reviewer prompt")
	}
}

func TestCanRunTeamGating(t *testing.T) {
	r := NewProjectRoom("team-gate", nil, nil, nil)
	if r.canRunTeam(teamTestPrompts()) {
		t.Fatal("nil engine must not run the team")
	}
}

func TestTeamToolsLeastPrivilege(t *testing.T) {
	r := NewProjectRoom("team-tools", nil, nil, nil)
	coderTools, reviewerTools, err := r.teamTools()
	if err != nil {
		t.Fatalf("teamTools: %v", err)
	}
	if len(coderTools) != 3 {
		t.Fatalf("coder needs write+read+list, got %d", len(coderTools))
	}
	if len(reviewerTools) != 2 {
		t.Fatalf("reviewer needs read+list only, got %d", len(reviewerTools))
	}
	names := map[string]bool{}
	for _, tl := range reviewerTools {
		info, err := tl.Info(nil)
		if err != nil {
			t.Fatalf("reviewer tool info: %v", err)
		}
		names[info.Name] = true
	}
	if names["vfs_write"] {
		t.Fatal("reviewer must never receive vfs_write")
	}
	if !names["vfs_read"] || !names["vfs_list"] {
		t.Fatalf("reviewer missing read/list: %v", names)
	}
}

func TestParseReviewVerdict(t *testing.T) {
	for _, tc := range []struct {
		in      string
		verdict string
		ok      bool
	}{
		{"APPROVE\npublic/index.html: ok", "approve", true},
		{"request_changes\n- public/js/main.js: fix", "request_changes", true},
		{"built three files", "", false},
	} {
		v, ok := parseReviewVerdict(tc.in)
		if v != tc.verdict || ok != tc.ok {
			t.Fatalf("parseReviewVerdict(%q) = %q,%v", tc.in, v, ok)
		}
	}
	if got := truncateRunes("abcdef", 3); got != "abc" {
		t.Fatalf("truncateRunes = %q", got)
	}
}
