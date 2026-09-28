import type { PreviewType } from "worker/types/sandbox-types";
import type { ImageAttachment } from '../../../types/image-attachment';
import type { BehaviorType, ProjectType } from 'worker/types/agent-core-types';
import type { CredentialsPayload } from 'worker/types/infer-config.types';

export const MAX_AGENT_QUERY_LENGTH = 20_000;

export interface CodeGenArgs {
    query: string;
    language?: string;
    frameworks?: string[];
    selectedTemplate?: string;
    behaviorType?: BehaviorType;
    projectType?: ProjectType;
    images?: ImageAttachment[];

    /** Optional ephemeral credentials (BYOK / gateway override) for sdk */
    credentials?: CredentialsPayload;
}

/**
 * Data structure for connectToExistingAgent response
 */
export interface AgentConnectionData {
    websocketUrl: string;
    agentId: string;
}

export type AgentPreviewResponse = PreviewType;
