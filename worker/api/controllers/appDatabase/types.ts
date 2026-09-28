/** Inlined from the removed `space` package (SpaceDO durable-object types). */
export interface AppDatabaseColumn {
	name: string
	type: string
	notnull: number
	pk: number
}
export interface AppDatabaseTable {
	name: string
	rowCount: number
	columns: AppDatabaseColumn[]
}
export interface AppDatabaseReadResult {
	columns: string[]
	rows: Record<string, unknown>[]
	totalCount: number
}

export interface ListAppTablesResponse {
	branch: string;
	tables: AppDatabaseTable[];
}

export interface QueryAppTableResponse {
	branch: string;
	table: string;
	columns: string[];
	rows: Record<string, unknown>[];
	totalCount: number;
	limit: number;
	offset: number;
}

export interface WipeAppDatabaseResponse {
	ok: true;
}
