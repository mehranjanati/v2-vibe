/**
 * Centralized API types - imports and re-exports types from worker
 * This file serves as the single source of truth for frontend-worker API communication
 */
import { SessionResponse } from 'worker/types/auth-types';
import { AuthUser } from './api-types';

export type { SecretTemplate } from 'worker/types/secretsTemplates';

// Base API Response Types
export type { ControllerResponse, ApiResponse } from 'worker/api/controllers/types';

// Database Types
export type {
  PaginationInfo,
  EnhancedAppData,
  AppWithFavoriteStatus,
  TimePeriod,
  AppSortOption,
  SortOrder,
  AppQueryOptions,
  PublicAppQueryOptions
} from 'worker/database/types';

// App-related API Types
export type { 
  AppsListData,
  PublicAppsData, 
  SingleAppData,
  FavoriteToggleData,
  CreateAppData,
  UpdateAppVisibilityData,
  AppDeleteData,
  AppWithUserAndStats
} from 'worker/api/controllers/apps/types';

export type {
  AppDetailsData,
  AppStarToggleData,
  GeneratedCodeFile,
  GitCloneTokenData,
  PreviewTokenData
} from 'worker/api/controllers/appView/types';

// User-related API Types
export type {
  UserAppsData,
  ProfileUpdateData,
} from 'worker/api/controllers/user/types';

// Stats API Types
export type {
  UserStatsData,
  UserActivityData
} from 'worker/api/controllers/stats/types';

// Analytics API Types
export type {
  UserAnalyticsResponseData,
  AgentAnalyticsResponseData,
} from 'worker/api/controllers/analytics/types';

export type { PlatformStatusData } from 'worker/api/controllers/status/types';

export type { CapabilitiesData } from 'worker/api/controllers/capabilities/types';

export type {
  ViewMode,
  FeatureCapabilities,
  FeatureDefinition,
  ViewDefinition,
  PlatformCapabilities,
  PlatformCapabilitiesConfig,
} from 'worker/types/agent-features';

export {
  DEFAULT_FEATURE_DEFINITIONS,
  getBehaviorTypeForProject,
} from 'worker/types/agent-features';

// Model Config API Types
export type {
  ModelConfigsData,
  ModelConfigData,
  ModelConfigUpdateData,
  ModelConfigTestData,
  ModelConfigResetData,
  ModelConfigDefaultsData,
  ModelConfigDeleteData,
  ByokProvidersData,
  UserProviderStatus,
  ModelsByProvider
} from 'worker/api/controllers/modelConfig/types';

// Model Provider API Types
export type {
  ModelProvidersListData,
  ModelProviderData,
  ModelProviderCreateData,
  ModelProviderUpdateData,
  ModelProviderDeleteData,
  ModelProviderTestData,
  CreateProviderRequest,
  UpdateProviderRequest,
  TestProviderRequest
} from 'worker/api/controllers/modelProviders/types';

// Frontend model config update interface that matches backend schema
export interface ModelConfigUpdate {
  modelName?: string | null;
  maxTokens?: number | null;
  temperature?: number | null;
  reasoningEffort?: string | null;
  fallbackModel?: string | null;
  isUserOverride?: boolean;
}

// Secrets API Types
export type { SecretTemplatesData } from 'worker/api/controllers/secrets/types';

// Vault API Types
export type {
	VaultConfig,
	VaultConfigResponse,
	VaultStatusResponse,
	SetupVaultRequest,
	KdfAlgorithm,
	Argon2Params,
	SecretMetadata,
} from 'worker/services/secrets/vault-types';

// Agent/CodeGen API Types
export type {
  AgentConnectionData,
} from 'worker/api/controllers/agent/types';

// Template Types
export type {
  TemplateDetails,
} from 'worker/types/sandbox-types';

// WebSocket Types
export type {
  WebSocketMessage,
  WebSocketMessageData,
  CodeFixEdits,
  ModelConfigsInfoMessage,
  AgentDisplayConfig,
  ModelConfigsInfo,
  CloudflareDeploymentErrorCode
} from 'worker/api/websocketTypes';

// Database/Schema Types commonly used in frontend
export type { 
  App,
  User,
  UserModelConfig,
  UserModelProvider
} from 'worker/database/schema';

export type {
  FavoriteToggleResult,
  UserStats,
  UserActivity,
  UserModelConfigWithMetadata,
  ModelTestResult
} from 'worker/database/types';

// Agent/Generator Types
export type {
  Blueprint as BlueprintType,
  PhasicBlueprint,
  CodeReviewOutputType,
  FileConceptType,
  FileOutputType as GeneratedFile,
} from 'worker/types/agent-schemas';

export type {
  AgentState,
  PhasicState
} from 'worker/types/agent-state';

export type {
  BehaviorType,
  ProjectType
} from 'worker/types/agent-core-types';
export { isAgenticLikeBehavior } from 'worker/types/agent-core-types';

export type {
  ConversationMessage,
} from 'worker/types/infer-common';

export type { 
  RuntimeError,
  StaticAnalysisResponse 
} from 'worker/types/sandbox-types';

// Config/Inference Types
export type { 
  AgentActionKey,
  AgentConfig,
  ModelConfig,
  ReasoningEffortType as ReasoningEffort,
  ProviderOverrideType as ProviderOverride
} from 'worker/types/infer-config.types';

export type { RateLimitError } from "worker/types/rate-limit";
export type { AgentPreviewResponse, CodeGenArgs } from 'worker/api/controllers/agent/types';
export { MAX_AGENT_QUERY_LENGTH } from 'worker/api/controllers/agent/types';

// App Database (DB tab) types
export type {
  AppDatabaseColumn,
  AppDatabaseTable,
  AppDatabaseReadResult,
  ListAppTablesResponse,
  QueryAppTableResponse,
  WipeAppDatabaseResponse,
} from 'worker/api/controllers/appDatabase/types';
export type { RateLimitErrorResponse } from 'worker/api/responses';
export { RateLimitExceededError, SecurityError, SecurityErrorType } from '../shared/types/errors.js';

export type { AIModels } from 'worker/types/infer-config.types';
// Model selection types
export type ModelSelectionMode = 'platform' | 'byok' | 'custom';

// Match chat FileType interface
export interface FileType {
	filePath: string;
	fileContents: string;
	explanation?: string;
	isGenerating?: boolean;
	needsFixing?: boolean;
	hasErrors?: boolean;
	language?: string;
}

// Streaming response wrapper types for agent session creation
export interface StreamingResponse {
  success: boolean;
  stream: Response;
}

export type AgentStreamingResponse = StreamingResponse;

export {
	type ImageAttachment, 
	isSupportedImageType, 
	MAX_IMAGE_SIZE_BYTES,
	MAX_IMAGES_PER_MESSAGE,
	SUPPORTED_IMAGE_MIME_TYPES
} from 'worker/types/image-attachment';

// Auth types imported from worker
export type { 
  AuthSession, 
  ApiKeyInfo, 
  AuthResult, 
  AuthUser,
  OAuthProvider 
} from 'worker/types/auth-types';
export type { 
  SessionResponse 
} from 'worker/types/auth-types';

// Auth API Response Types (using existing worker types)
export type LoginResponseData = SessionResponse;

export type RegisterResponseData = SessionResponse & {
  requiresVerification?: boolean;
};

export type ProfileResponseData = {
  user: AuthUser;
  sessionId: string;
};

export interface AuthProvidersResponseData {
  providers: {
    google: boolean;
    github: boolean;
    cloudflare: boolean;
    email: boolean;
  };
  hasOAuth: boolean;
  requiresEmailAuth: boolean;
  csrfToken?: string;
  csrfExpiresIn?: number;
}

export interface CsrfTokenResponseData {
  token: string;
  headerName: string;
  expiresIn?: number;
}

// Active Sessions Response - matches getUserSessions + isCurrent from controller
export interface ActiveSessionsData {
  sessions: Array<{
    id: string;
    userAgent: string | null;
    ipAddress: string | null;
    lastActivity: Date;
    createdAt: Date;
    isCurrent: boolean;
  }>;
}

// Linked OAuth Identities Response - matches getLinkedIdentities controller format
export interface LinkedIdentitiesData {
  identities: Array<{
    provider: string;
    email: string | null;
    emailVerified: boolean;
    createdAt: Date | null;
  }>;
}

// API Keys Response - matches controller response format
export interface ApiKeysData {
  keys: Array<{
    id: string;
    name: string;
    keyPreview: string;
    createdAt: Date | null;
    lastUsed: Date | null;
    isActive: boolean;
  }>;
}

// ========================================
// Outcome Runs (M1.1)
// ========================================
//
// One `OutcomeRun` is the execution record of an outcome and mirrors the
// workflow-run contract 1:1 (`workflow_instances` + `workflow_step_logs`):
// the control plane records the pending run, the runtime advances it
// (pending → running → succeeded | failed | cancelled) and writes one
// `OutcomeRunStep` per node attempt. Payloads are decoded JSON columns
// (`decodeJSONField` in backend/pkg/api/workflows.go) and timestamps are
// epoch milliseconds, null while a run has not started/finished.
//
// Contract-first: the matching `/api/outcomes/*` control-plane routes land in
// a later M1 task; until then the api-client calls answer 404/503 instead of
// the SPA HTML.

/** In-flight + terminal states of an outcome run (workflow_instances.status). */
export type OutcomeRunStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled';

/** Per-step states of an outcome run (workflow_step_logs.status). */
export type OutcomeRunStepStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'skipped';

/** Node type executed by an outcome run step (WORKFLOW_NODE_TYPE_VALUES). */
export type OutcomeRunStepNodeType =
  | 'trigger'
  | 'http'
  | 'db'
  | 'ai'
  | 'email'
  | 'condition'
  | 'sleep';

/** Live Cloudflare Workflows status merged into a pending/running run. */
export type OutcomeRunCloudflareStatus =
  | 'queued'
  | 'running'
  | 'paused'
  | 'errored'
  | 'terminated'
  | 'complete';

/** Readiness of the stored outcome definition (workflow_dags.status). */
export type OutcomeRunDefinitionStatus = 'generated' | 'invalid';

/**
 * A decoded JSON column: object, array, raw string (scalars/non-JSON) or
 * null for SQL NULL — the values `decodeJSONField` returns.
 */
export type OutcomeRunPayload =
  | Record<string, unknown>
  | unknown[]
  | string
  | null;

/**
 * One step log of an outcome run. Mirrors `buildStepMap` in
 * backend/pkg/api/workflows.go (`workflow_step_logs` row).
 */
export interface OutcomeRunStep {
  id: string;
  /** DAG node id; maps 1:1 to the Workflow step name at runtime. */
  stepName: string;
  nodeType: OutcomeRunStepNodeType | null;
  status: OutcomeRunStepStatus;
  /** 1-based attempt counter (retries produce additional rows). */
  attempt: number;
  input: OutcomeRunPayload;
  output: OutcomeRunPayload;
  error: string | null;
  startedAt: number | null;
  completedAt: number | null;
}

/**
 * One outcome run. Mirrors `buildInstanceMap` in
 * backend/pkg/api/workflows.go (`workflow_instances` row); tenant scope
 * (`user_id`) stays server-side and is never part of the response.
 */
export interface OutcomeRun {
  id: string;
  outcomeId: string;
  status: OutcomeRunStatus;
  input: OutcomeRunPayload;
  output: OutcomeRunPayload;
  error: string | null;
  startedAt: number | null;
  completedAt: number | null;
  createdAt: number | null;
  /** Number of step-log rows; present on the single-run response only. */
  stepCount?: number;
  /** Present only while the run is pending/running and Cloudflare answers. */
  cloudflareStatus?: OutcomeRunCloudflareStatus;
}

/**
 * The outcome a run belongs to. Mirrors the `workflow` object of
 * GET /api/workflows/:workflowId (`workflow_dags` row + parsed DAG).
 */
export interface OutcomeRunDefinition {
  outcomeId: string;
  schemaVersion: number;
  status: OutcomeRunDefinitionStatus;
  updatedAt: number | null;
  /** Compiled plan/DAG; malformed JSON comes back as the raw string. */
  dag: OutcomeRunPayload;
}

/** Response of POST /api/outcomes/trigger (mirrors the workflow trigger). */
export interface OutcomeRunTriggerData {
  outcomeId: string;
  runId: string;
  status: OutcomeRunStatus;
}

/**
 * Response of GET /api/outcomes/:outcomeId — the outcome plus its most
 * recent runs, newest first (server caps the list at 20).
 */
export interface OutcomeRunsData {
  outcome: OutcomeRunDefinition;
  runs: OutcomeRun[];
}

/**
 * Response of GET /api/outcomes/:outcomeId?runId=<id> — one run plus its
 * per-step logs in execution order.
 */
export interface OutcomeRunDetailData {
  outcome: OutcomeRunDefinition;
  run: OutcomeRun;
  steps: OutcomeRunStep[];
}

export type {
    GitHubExportOptions,
    GitHubExportResult,
} from 'worker/types/github-types';
