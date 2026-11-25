CREATE TABLE `activation_decision_traces` (
	`run_id` text PRIMARY KEY NOT NULL,
	`tenant_key` text NOT NULL,
	`context_id` text NOT NULL,
	`composition_digest` text NOT NULL,
	`activation_policy_ref` text NOT NULL,
	`ranking_source` text NOT NULL,
	`ranking_version` text,
	`decided_at` integer,
	`trace_digest` text NOT NULL,
	`record` text NOT NULL
);
--> statement-breakpoint
CREATE INDEX `activation_decision_traces_tenant_idx` ON `activation_decision_traces` (`tenant_key`);