package engine

import (
	"encoding/json"
	"log"

	"github.com/google/uuid"

	agentplan "backend/agent"
	"backend/pkg/cloudflare"
)

// ---------- P1.8 difficulty gate ----------
//
// The dual-model pipeline can execute an approved plan two ways: the
// multi-agent team (coordinator → coder → reviewer, three models and a tool
// loop each) or the single-coder step loop. The team is the right call for a
// plan that is genuinely made of coupled parts and the wrong call for "give me
// one page": it multiplies token spend and latency for a task one coder call
// finishes.
//
// The gate therefore answers one question before the run picks a path — is this
// plan small AND independent? — and records the answer in `generation_audits`
// (actor=system, action=gate_decision) so "why did this run use the team?" is
// answerable from D1 alone, not from a log line that has rotated away.
//
// Note on the coupling signal: `agentplan.PlanStep` carries no explicit
// dependency edges. The two signals it does give are `AssociatedSubtaskIndex`
// (steps in the same subtask belong to one coherent unit, so they are coupled)
// and `Action` (a `modify`/`delete` step depends on state an earlier step
// wrote). Anything else would be guessing.

const (
	// teamGateMaxSingleCoderSteps is the P1.8 budget: at most this many steps
	// with zero coupling stays on the single-coder loop.
	teamGateMaxSingleCoderSteps = 2
	// gateDecisionAction is the generation_audits action of the routing record.
	gateDecisionAction = "gate_decision"
)

// teamGateDecision is the routing decision plus the numbers behind it, so the
// audit row explains itself.
type teamGateDecision struct {
	UseTeam      bool
	Steps        int
	Dependencies int
	Reason       string
}

// teamGate resolves the decision for this room: the difficulty rules plus
// whether the team is actually usable (engine, tool-calling model, prompts).
func (r *ProjectRoom) teamGate(plan agentplan.ExecutionPlan) teamGateDecision {
	return newTeamGateDecision(plan, r.canRunTeam(r.teamPrompts))
}

// newTeamGateDecision is the pure rule set, separated from the room so the
// difficulty cases are testable without an engine.
func newTeamGateDecision(plan agentplan.ExecutionPlan, teamAvailable bool) teamGateDecision {
	decision := teamGateDecision{
		Steps:        len(plan.Steps),
		Dependencies: planDependencyCount(plan),
	}
	if !teamAvailable {
		decision.Reason = "team unavailable (engine, tool model or role prompts)"
		return decision
	}
	switch {
	case decision.Steps > teamGateMaxSingleCoderSteps:
		decision.UseTeam = true
		decision.Reason = "plan has more steps than the single-coder budget"
	case decision.Dependencies > 0:
		decision.UseTeam = true
		decision.Reason = "steps depend on each other"
	default:
		decision.Reason = "small independent plan"
	}
	return decision
}

// planDependencyCount counts the steps that cannot run in isolation: one that
// modifies or deletes existing state, or one that shares its subtask with
// another step.
func planDependencyCount(plan agentplan.ExecutionPlan) int {
	if len(plan.Steps) == 0 {
		return 0
	}
	perSubtask := make(map[int]int, len(plan.Steps))
	for _, step := range plan.Steps {
		perSubtask[step.AssociatedSubtaskIndex]++
	}
	dependencies := 0
	for _, step := range plan.Steps {
		if step.Action != "" && step.Action != "create" {
			dependencies++
			continue
		}
		if perSubtask[step.AssociatedSubtaskIndex] > 1 {
			dependencies++
		}
	}
	return dependencies
}

// writeGateDecisionAudit records the routing decision (P1.8). Best-effort and
// detached, like the other lineage writers: a missing D1 client or a failing
// insert must never change which path the run takes.
func writeGateDecisionAudit(d1 *cloudflare.D1Client, genID string, decision teamGateDecision) {
	if d1 == nil || genID == "" {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[generation:%s] gate audit writer panic recovered: %v", genID, rec)
			}
		}()
		path := "single_coder"
		if decision.UseTeam {
			path = "team"
		}
		payload, err := json.Marshal(map[string]interface{}{
			"decision":     path,
			"steps":        decision.Steps,
			"dependencies": decision.Dependencies,
			"reason":       decision.Reason,
		})
		if err != nil {
			payload = []byte("{}")
		}
		ctx, cancel := lineageCtx(nil)
		defer cancel()
		if _, err := d1.Exec(ctx, generationAuditInsertSQL, uuid.NewString(), genID, "system", gateDecisionAction, string(payload)); err != nil {
			log.Printf("[generation:%s] audit row %s failed: %v", genID, gateDecisionAction, err)
		}
	}()
}
