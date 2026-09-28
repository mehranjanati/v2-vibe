package models

// Protocol contract mirroring the TypeScript union in
// worker/api/websocketTypes.ts (read-only reference). Only the subset
// required by the React frontend's connection lifecycle and VFS sync is
// implemented here; extend as needed.

// ---------- Server -> Client events ----------

// CFAgentStateEvent mirrors `cf_agent_state` (StateMessage).
type CFAgentStateEvent struct {
	Type  string      `json:"type"`
	State *AgentState `json:"state"`
}

// AgentConnectedEvent mirrors `agent_connected` (AgentConnectedMessage).
type AgentConnectedEvent struct {
	Type            string          `json:"type"`
	State           *AgentState     `json:"state"`
	TemplateDetails *TemplateDetail `json:"templateDetails"`
	PreviewURL      string          `json:"previewUrl,omitempty"`
}

// FileChunkGenerated mirrors `file_chunk_generated` (FileChunkGeneratedMessage).
type FileChunkGenerated struct {
	Type     string `json:"type"`
	FilePath string `json:"filePath"`
	Chunk    string `json:"chunk"`
}

// FileGenerated mirrors `file_generated` (FileGeneratedMessage).
type FileGenerated struct {
	Type string     `json:"type"`
	File *FileEntry `json:"file"`
}

// FileDeleted mirrors `file_deleted` (FileDeletedMessage): the file was
// removed from the VFS (dual-model pipeline delete steps).
type FileDeleted struct {
	Type     string `json:"type"`
	FilePath string `json:"filePath"`
}

// FileGenerating mirrors `file_generating` (FileGeneratingMessage).
type FileGenerating struct {
	Type        string `json:"type"`
	FilePath    string `json:"filePath"`
	FilePurpose string `json:"filePurpose"`
}

// GenerationStarted mirrors `generation_started` (GenerationStartedMessage).
type GenerationStarted struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	TotalFiles int    `json:"totalFiles"`
}

// GenerationComplete mirrors `generation_complete` (GenerationCompleteMessage).
type GenerationComplete struct {
	Type       string `json:"type"`
	InstanceID string `json:"instanceId,omitempty"`
	PreviewURL string `json:"previewURL,omitempty"`
}

// GenerationInterrupted is an additive event (B12) signalling the
// generation did NOT finish cleanly: some files may be partial
// (stream-salvaged) or missing. reason is a short human string, e.g.
// "stream error" or "token limit reached after retries".
type GenerationInterrupted struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// GenerationCancelled is an additive event (B9) emitted when the user (or
// room shutdown) cancelled an in-flight generation. Streamed files up to
// the cancellation point remain in the VFS.
type GenerationCancelled struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// PlanProposed is an additive event (B7, human-in-the-loop): the build
// plan is ready and the room pauses until the client replies with
// plan_approved (proceed) or plan_rejected (abort). conversationId is the
// chat conversation the plan streamed into.
type PlanProposed struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversationId"`
	Plan           string `json:"plan"`
}

// PlanStructure is an additive event (R3 dual-model pipeline): the
// machine-readable plan arrays for structured frontend rendering — the
// coarse subtasks plus the ordered per-file steps with their actions. It
// is broadcast right after the plan is validated, before plan_proposed.
type PlanStructure struct {
	Type     string              `json:"type"` // "plan_structure"
	Goal     string              `json:"goal,omitempty"`
	Subtasks []string            `json:"subtasks"`
	Steps    []PlanStructureStep `json:"steps"`
}

// PlanStructureStep is one executable file instruction in a PlanStructure.
// The JSON keys mirror the ExecutionPlan wire contract (backend/agent).
type PlanStructureStep struct {
	Action                 string `json:"action"` // "create", "modify", "delete"
	FilePath               string `json:"file_path"`
	Description            string `json:"description"`
	AssociatedSubtaskIndex int    `json:"associated_subtask_index"`
}

// DeploymentStarted mirrors `deployment_started` (DeploymentStartedMessage).
type DeploymentStarted struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Files   []struct {
		FilePath string `json:"filePath"`
	} `json:"files"`
}

// DeploymentFailed mirrors `deployment_failed` (DeploymentFailedMessage).
type DeploymentFailed struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// DeployProgress is an additive progress event emitted during a Pages
// deployment. The React frontend logs unknown types harmlessly, so this
// is safe to add alongside the existing deployment_* events.
type DeployProgress struct {
	Type     string `json:"type"`
	Message  string `json:"message"`
	Progress int    `json:"progress"` // 0-100
}

// DeploymentCompleted mirrors `deployment_completed` (DeploymentCompletedMessage).
type DeploymentCompleted struct {
	Type       string `json:"type"`
	PreviewURL string `json:"previewURL"`
	TunnelURL  string `json:"tunnelURL"`
	InstanceID string `json:"instanceId"`
	Message    string `json:"message"`
}

// CloudflareDeploymentCompleted mirrors `cloudflare_deployment_completed`.
type CloudflareDeploymentCompleted struct {
	Type          string `json:"type"`
	Message       string `json:"message"`
	InstanceID    string `json:"instanceId"`
	DeploymentURL string `json:"deploymentUrl"`
	WorkersURL    string `json:"workersUrl,omitempty"`
}

// TeamStarted mirrors `team_started`: the multi-agent team
// (coordinator/coder/reviewer) began executing the approved plan.
type TeamStarted struct {
	Type string `json:"type"`
}

// SubAgentActivity mirrors `subagent_activity`: a transparent delegation
// trace — which sub-agent called which tool. Render-only, never touches
// the VFS.
type SubAgentActivity struct {
	Type      string `json:"type"`
	AgentName string `json:"agentName"`
	ToolName  string `json:"toolName"`
}

// TeamCompleted mirrors `team_completed`: the team finished. Verdict is
// one of "approve", "request_changes", "done" (no explicit verdict) or
// "error" (see Summary).
type TeamCompleted struct {
	Type    string `json:"type"`
	Verdict string `json:"verdict"`
	Summary string `json:"summary,omitempty"`
}

// ErrorEvent mirrors `error` (ErrorMessage).
type ErrorEvent struct {
	Type        string `json:"type"`
	Error       string `json:"error"`
	Code        string `json:"code,omitempty"`
	ShowAsPopup bool   `json:"showAsPopup,omitempty"`
}

// ConversationResponse mirrors `conversation_response`
// (ConversationResponseMessage) — a streamed conversational reply. It is
// emitted once per LLM delta with IsStreaming=true; a final (non-streaming)
// event closes the turn so the frontend finalizes the assistant message.
// Unlike file generation events, it never touches shouldBeGenerating nor the
// VFS — it only renders as markdown in the chat thread.
type ConversationResponse struct {
	Type           string `json:"type"`
	Message        string `json:"message"`
	ConversationID string `json:"conversationId,omitempty"`
	IsStreaming    bool   `json:"isStreaming,omitempty"`
}

// ---------- Client -> Server messages ----------

// ClientMessage is the generic envelope the React frontend sends.
// Outbound types observed: get_conversation_state, generate_all,
// stop_generation, resume_generation, deploy, preview,
// capture_screenshot, clear_conversation, rollback_to_commit,
// user_suggestion.
type ClientMessage struct {
	Type       string `json:"type"`
	InstanceID string `json:"instanceId,omitempty"`
	Target     string `json:"target,omitempty"` // 'platform' | 'user'
	URL        string `json:"url,omitempty"`
	CommitHash string `json:"commitHash,omitempty"`
	Message    string `json:"message,omitempty"`
	Command    string `json:"command,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// ---------- Shared value types ----------

// AgentState mirrors the AgentState shape the frontend reads
// (behaviorType, projectType, generatedFilesMap, shouldBeGenerating, ...).
type AgentState struct {
	BehaviorType       string                `json:"behaviorType,omitempty"`
	ProjectType        string                `json:"projectType,omitempty"`
	Blueprint          map[string]any        `json:"blueprint,omitempty"`
	Query              string                `json:"query,omitempty"`
	GeneratedFilesMap  map[string]*FileEntry `json:"generatedFilesMap,omitempty"`
	ShouldBeGenerating bool                  `json:"shouldBeGenerating,omitempty"`
	PendingUserInputs  []string              `json:"pendingUserInputs,omitempty"`
}

// FileEntry is a single file in the VFS.
type FileEntry struct {
	FilePath     string `json:"filePath"`
	FileContents string `json:"fileContents"`
}

// TemplateDetail mirrors the TemplateDetails the frontend uses for
// bootstrap file restoration.
type TemplateDetail struct {
	RenderMode     string            `json:"renderMode,omitempty"`
	SlideDirectory string            `json:"slideDirectory,omitempty"`
	ImportantFiles []string          `json:"importantFiles,omitempty"`
	AllFiles       map[string]string `json:"allFiles,omitempty"`
}
