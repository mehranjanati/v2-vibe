CREATE TABLE `generation_audits` (
	`id` text PRIMARY KEY NOT NULL,
	`generation_id` text NOT NULL,
	`actor` text NOT NULL,
	`action` text NOT NULL,
	`detail_json` text,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (`generation_id`) REFERENCES `generations`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `generation_audits_generation_idx` ON `generation_audits` (`generation_id`,`created_at`);--> statement-breakpoint
CREATE INDEX `generation_audits_action_idx` ON `generation_audits` (`action`);--> statement-breakpoint
CREATE TABLE `generation_files` (
	`generation_id` text NOT NULL,
	`path` text NOT NULL,
	`op` text NOT NULL,
	`before_hash` text,
	`after_hash` text,
	`before_size` integer,
	`after_size` integer,
	`author_agent` text,
	PRIMARY KEY(`generation_id`, `path`),
	FOREIGN KEY (`generation_id`) REFERENCES `generations`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `generation_files_path_idx` ON `generation_files` (`path`);--> statement-breakpoint
CREATE TABLE `generations` (
	`id` text PRIMARY KEY NOT NULL,
	`chat_id` text NOT NULL,
	`parent` text,
	`fork` integer DEFAULT false NOT NULL,
	`commit_sha` text,
	`branch` text,
	`verdict` text,
	`status` text DEFAULT 'running' NOT NULL,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (`parent`) REFERENCES `generations`(`id`) ON UPDATE no action ON DELETE set null
);
--> statement-breakpoint
CREATE INDEX `generations_chat_created_at_idx` ON `generations` (`chat_id`,`created_at`);--> statement-breakpoint
CREATE INDEX `generations_parent_idx` ON `generations` (`parent`);--> statement-breakpoint
CREATE INDEX `generations_status_idx` ON `generations` (`status`);