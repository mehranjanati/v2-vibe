package toolsearch

import (
	"fmt"
	"strings"
)

// Sandbox is the only thing that can run agent-authored code. The interface
// exists so this package can never execute a program directly: it holds a
// Sandbox or it holds nothing.
//
// A real implementation is a WASM isolate on the Edge for hosted workloads, or a
// container where we control the host (research log, proposal 5). That work is
// sequenced with the execution provider (P7-RUN) and is deliberately not
// implemented here — this file is the gate, not the runtime.
type Sandbox interface {
	// Run executes one program inside the sandbox and returns its output.
	Run(program string) (string, error)
}

// CodeMode is the P7-SCH-001 spike gate for Code Mode: expose two tools that run
// agent-written code instead of one tool per endpoint.
//
// It is off unless both conditions hold — an explicit `enabled` flag and a
// Sandbox — so a misconfiguration degrades to "refused" instead of "arbitrary
// code execution". The zero value is inert: Run on a nil receiver refuses.
type CodeMode struct {
	sandbox Sandbox
}

// NewCodeMode builds the spike. It returns an error, not a usable value, when
// the flag is off or the sandbox is missing: a caller cannot accidentally hold a
// live Code Mode handle.
func NewCodeMode(enabled bool, sandbox Sandbox) (*CodeMode, error) {
	if !enabled {
		return nil, fmt.Errorf("toolsearch: code mode is disabled; set the explicit sandbox flag only after the execution provider is ready")
	}
	if sandbox == nil {
		return nil, fmt.Errorf("toolsearch: code mode requires a sandbox (WASM isolate or container); refusing to expose remote code execution")
	}
	return &CodeMode{sandbox: sandbox}, nil
}

// Run executes a program through the sandbox. It refuses on a nil handle, an
// empty program, or a handle that somehow lost its sandbox.
func (c *CodeMode) Run(program string) (string, error) {
	if c == nil || c.sandbox == nil {
		return "", fmt.Errorf("toolsearch: code mode has no sandbox; refusing to run agent-authored code")
	}
	if strings.TrimSpace(program) == "" {
		return "", fmt.Errorf("toolsearch: code mode: empty program")
	}
	return c.sandbox.Run(program)
}
