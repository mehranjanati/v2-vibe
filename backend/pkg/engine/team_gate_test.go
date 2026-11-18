package engine

import (
	"strings"
	"testing"

	agentplan "backend/agent"
)

func gateStep(action, path string, subtask int) agentplan.PlanStep {
	return agentplan.PlanStep{
		Action:                 action,
		FilePath:               path,
		Description:            action + " " + path,
		AssociatedSubtaskIndex: subtask,
	}
}

func TestTeamGateSingleFilePlanStaysSingleCoder(t *testing.T) {
	d := newTeamGateDecision(agentplan.ExecutionPlan{
		Steps: []agentplan.PlanStep{gateStep("create", "index.html", 0)},
	}, true)
	if d.UseTeam {
		t.Fatalf("single independent file must stay on the single coder, got %+v", d)
	}
	if d.Steps != 1 || d.Dependencies != 0 {
		t.Fatalf("unexpected counts: %+v", d)
	}
}

func TestTeamGateFourFilePlanUsesTeam(t *testing.T) {
	d := newTeamGateDecision(agentplan.ExecutionPlan{
		Steps: []agentplan.PlanStep{
			gateStep("create", "index.html", 0),
			gateStep("create", "styles.css", 0),
			gateStep("create", "main.js", 1),
			gateStep("create", "api.js", 1),
		},
	}, true)
	if !d.UseTeam {
		t.Fatalf("four coupled files must use the team, got %+v", d)
	}
	if d.Reason == "" {
		t.Error("a decision must carry its reason")
	}
}

// TestTeamGateBoundaryTwoCoupledFilesUsesTeam is the spec's case (3): two files
// is within the step budget, but coupling alone must send them to the team.
func TestTeamGateBoundaryTwoCoupledFilesUsesTeam(t *testing.T) {
	d := newTeamGateDecision(agentplan.ExecutionPlan{
		Steps: []agentplan.PlanStep{
			gateStep("create", "index.html", 0),
			gateStep("create", "styles.css", 0),
		},
	}, true)
	if !d.UseTeam {
		t.Fatalf("two files in one subtask are coupled, want the team; got %+v", d)
	}
	if d.Dependencies == 0 {
		t.Fatalf("coupling was not detected: %+v", d)
	}
}

func TestTeamGateTwoIndependentFilesStaySingleCoder(t *testing.T) {
	d := newTeamGateDecision(agentplan.ExecutionPlan{
		Steps: []agentplan.PlanStep{
			gateStep("create", "index.html", 0),
			gateStep("create", "about.html", 1),
		},
	}, true)
	if d.UseTeam {
		t.Fatalf("two independent files fit the single coder, got %+v", d)
	}
}

func TestTeamGateUnavailableTeamNeverRoutesToTeam(t *testing.T) {
	plan := agentplan.ExecutionPlan{
		Steps: []agentplan.PlanStep{
			gateStep("create", "index.html", 0),
			gateStep("create", "styles.css", 0),
			gateStep("create", "main.js", 1),
		},
	}
	d := newTeamGateDecision(plan, false)
	if d.UseTeam {
		t.Fatalf("no engine/prompts must never select the team, got %+v", d)
	}
	if !strings.Contains(d.Reason, "unavailable") {
		t.Errorf("reason %q should say the team is unavailable", d.Reason)
	}

	// The same plan through the room (nil engine) reaches the same conclusion.
	room := NewProjectRoom("gate-room", nil, nil, nil)
	if got := room.teamGate(plan); got.UseTeam {
		t.Fatalf("room gate without an engine = %+v, want single coder", got)
	}
}

func TestPlanDependencyCount(t *testing.T) {
	cases := []struct {
		name string
		plan agentplan.ExecutionPlan
		want int
	}{
		{"empty plan", agentplan.ExecutionPlan{}, 0},
		{"one create", agentplan.ExecutionPlan{Steps: []agentplan.PlanStep{
			gateStep("create", "a.html", 0),
		}}, 0},
		{"two creates in different subtasks", agentplan.ExecutionPlan{Steps: []agentplan.PlanStep{
			gateStep("create", "a.html", 0),
			gateStep("create", "b.html", 1),
		}}, 0},
		{"two creates in one subtask", agentplan.ExecutionPlan{Steps: []agentplan.PlanStep{
			gateStep("create", "a.html", 0),
			gateStep("create", "b.html", 0),
		}}, 2},
		{"modify depends on prior state", agentplan.ExecutionPlan{Steps: []agentplan.PlanStep{
			gateStep("create", "a.html", 0),
			gateStep("modify", "b.html", 1),
		}}, 1},
		{"delete depends on prior state", agentplan.ExecutionPlan{Steps: []agentplan.PlanStep{
			gateStep("delete", "old.css", 0),
			gateStep("create", "new.css", 1),
		}}, 1},
	}
	for _, tc := range cases {
		if got := planDependencyCount(tc.plan); got != tc.want {
			t.Errorf("%s: planDependencyCount = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestGateDecisionAuditRecordsTheDecision is the spec's case (4): the routing
// decision lands in generation_audits with the numbers behind it.
func TestGateDecisionAuditRecordsTheDecision(t *testing.T) {
	room, fake := newGenerationCommitRoom(t)

	writeGateDecisionAudit(room.d1, "gen-gate", teamGateDecision{
		UseTeam:      true,
		Steps:        4,
		Dependencies: 4,
		Reason:       "steps depend on each other",
	})
	waitForGenerationAudit(t, fake, gateDecisionAction)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, req := range fake.requests {
		if !strings.HasPrefix(req.sql, "INSERT INTO generation_audits") {
			continue
		}
		var actor, action, detail string
		for _, p := range req.params {
			s, ok := p.(string)
			if !ok {
				continue
			}
			switch {
			case s == gateDecisionAction:
				action = s
			case s == "system":
				actor = s
			case strings.HasPrefix(s, "{"):
				detail = s
			}
		}
		if action != gateDecisionAction {
			continue
		}
		if actor != "system" {
			t.Errorf("gate decision actor = %q, want system", actor)
		}
		for _, want := range []string{`"decision":"team"`, `"steps":4`, `"dependencies":4`} {
			if !strings.Contains(detail, want) {
				t.Errorf("gate decision detail %q missing %s", detail, want)
			}
		}
		return
	}
	t.Fatal("no gate_decision row was written")
}
