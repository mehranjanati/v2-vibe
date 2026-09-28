/**
 * Code-fixer result types, inlined from the removed `worker/services/code-fixer`
 * service so the shared WebSocket protocol keeps compiling without the heavy plane.
 */

export interface FileObject {
	filePath: string;
	fileContents: string;
}

export interface FixedIssue {
	/** TypeScript error code (e.g., 'TS2307') */
	issueCode: string;
	/** File path where the issue was located */
	filePath: string;
	/** Line number of the issue */
	line: number;
	/** Column number of the issue (optional) */
	column?: number;
	/** Original error message from TypeScript */
	originalMessage: string;
	/** Description of the fix that was applied */
	fixApplied: string;
	/** Type of fix that was applied */
	fixType: 'import_fix' | 'export_fix' | 'stub_creation' | 'declaration_fix';
}

export interface UnfixableIssue {
	/** TypeScript error code (e.g., 'TS2307') */
	issueCode: string;
	/** File path where the issue was located */
	filePath: string;
	/** Line number of the issue */
	line: number;
	/** Column number of the issue (optional) */
	column?: number;
	/** Original error message from TypeScript */
	originalMessage: string;
	/** Reason why the issue could not be fixed */
	reason: string;
}

export interface CodeFixResult {
	fixedIssues: FixedIssue[];
	unfixableIssues: UnfixableIssue[];
	modifiedFiles: FileObject[];
	newFiles?: FileObject[];
}
