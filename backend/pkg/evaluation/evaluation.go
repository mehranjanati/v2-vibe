package evaluation

import (
	"errors"
	"fmt"
	"time"
)

// Layer is one of the five closed evaluation layers.
type Layer string

const (
	LayerStructural  Layer = "L1-structural"
	LayerCapability  Layer = "L2-capability"
	LayerBehavioral  Layer = "L3-behavioral"
	LayerOperational Layer = "L4-operational"
	LayerSafety      Layer = "L5-safety"
)

// allLayers is the closed set, in reporting order.
var allLayers = []Layer{LayerStructural, LayerCapability, LayerBehavioral, LayerOperational, LayerSafety}

// Verdict is the outcome of one layer.
type Verdict string

const (
	VerdictPass         Verdict = "pass"
	VerdictFail         Verdict = "fail"
	VerdictNotEvaluated Verdict = "not_evaluated"
	VerdictBlocked      Verdict = "blocked"
)

// Code is a contract violation that refuses the whole run.
type Code string

const (
	CodeSubject Code = "E_EVAL_SUBJECT"
	CodeJudge   Code = "E_EVAL_JUDGE"
	CodeDataset Code = "E_EVAL_DATASET"
)

// Judge declares an LLM judge. It may inform L3 only.
type Judge struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Version     string  `json:"version"`
	Temperature float64 `json:"temperature"`
	Seed        int64   `json:"seed"`
}

// Metric is one judged measurement.
type Metric struct {
	Name      string  `json:"name"`
	Direction string  `json:"direction"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Passed    bool    `json:"passed"`
}

// LayerResult is the evidence for one layer.
type LayerResult struct {
	Layer   Layer    `json:"layer"`
	Verdict Verdict  `json:"verdict"`
	Reason  string   `json:"reason,omitempty"`
	Metrics []Metric `json:"metrics"`
}

// Budget bounds the run. Deadline is the injected form of maxWallClock: the
// clock is injected, so an absolute deadline is reproducible where a wall-clock
// read would not be.
type Budget struct {
	MaxCases   int       `json:"maxCases"`
	MaxCostUSD float64   `json:"maxCostUSD"`
	Deadline   time.Time `json:"deadline,omitempty"`
}

// Request is everything the runner is allowed to look at.
type Request struct {
	SnapshotID string
	// SystemDocument is the System manifest: L1 needs the bytes, the snapshot
	// only stores their digest (benchmark-runner.md §2).
	SystemDocument   []byte
	Layers           []Layer
	Datasets         []string
	Thresholds       map[string]float64
	Budget           Budget
	Judge            *Judge
	Now              time.Time
	EvaluatorVersion string
}

// Result is the artifact: canonical bytes and a digest excluding the clock.
type Result struct {
	EvaluatorVersion string        `json:"evaluatorVersion"`
	SnapshotID       string        `json:"snapshotId"`
	SystemRef        string        `json:"systemRef"`
	SystemDigest     string        `json:"systemDigest"`
	GeneratedAt      string        `json:"generatedAt"`
	Layers           []LayerResult `json:"layers"`
	Verdict          Verdict       `json:"verdict"`
	Budget           Budget        `json:"budget"`
	Judge            *Judge        `json:"judge,omitempty"`
	Datasets         []string      `json:"datasets"`
	// Findings are contract violations; a non-empty list means no verdict at all.
	Findings []Finding `json:"findings"`
}

// Finding is one refusal.
type Finding struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s", f.Code, f.Message) }

// ErrSubject marks an unusable subject; callers map it to exit code 3.
var ErrSubject = errors.New("evaluation: subject is not evaluable")

// ValidLayer reports whether a layer name is in the closed set.
func ValidLayer(layer Layer) bool {
	for _, known := range allLayers {
		if known == layer {
			return true
		}
	}
	return false
}
