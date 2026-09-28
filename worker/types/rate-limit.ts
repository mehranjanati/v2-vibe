export enum RateLimitType {
	API_RATE_LIMIT = 'apiRateLimit',
	AUTH_RATE_LIMIT = 'authRateLimit',
	APP_CREATION = 'appCreation',
	LLM_CALLS = 'llmCalls',
	PUBLIC_APPS = 'publicApps',
	SPACE_PREVIEW = 'spacePreview',
}

export interface RateLimitError {
	message: string;
	limitType: RateLimitType;
	limit?: number;
	period?: number; // seconds
	suggestions?: string[];
}