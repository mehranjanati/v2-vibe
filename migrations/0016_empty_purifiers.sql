CREATE TABLE `trade_journal` (
	`id` text PRIMARY KEY NOT NULL,
	`tenant_id` text NOT NULL,
	`agent_name` text NOT NULL,
	`run_id` text NOT NULL,
	`event` text NOT NULL,
	`detail_json` text,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP
);
--> statement-breakpoint
CREATE INDEX `trade_journal_tenant_agent_idx` ON `trade_journal` (`tenant_id`,`agent_name`,`created_at`);--> statement-breakpoint
CREATE INDEX `trade_journal_run_idx` ON `trade_journal` (`run_id`);--> statement-breakpoint
CREATE INDEX `trade_journal_event_idx` ON `trade_journal` (`event`);--> statement-breakpoint
CREATE TABLE `trade_state` (
	`tenant_id` text NOT NULL,
	`agent_name` text NOT NULL,
	`state_json` text NOT NULL,
	`updated_run_id` text NOT NULL,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(`tenant_id`, `agent_name`)
);
