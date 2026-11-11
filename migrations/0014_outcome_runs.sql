CREATE TABLE `outcome_definitions` (
	`outcome_id` text PRIMARY KEY NOT NULL,
	`schema_version` integer DEFAULT 2 NOT NULL,
	`dag_json` text NOT NULL,
	`status` text DEFAULT 'generated' NOT NULL,
	`user_id` text,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP
);
--> statement-breakpoint
CREATE INDEX `outcome_definitions_status_idx` ON `outcome_definitions` (`status`);--> statement-breakpoint
CREATE INDEX `outcome_definitions_updated_at_idx` ON `outcome_definitions` (`updated_at`);--> statement-breakpoint
CREATE INDEX `outcome_definitions_user_idx` ON `outcome_definitions` (`user_id`);--> statement-breakpoint
CREATE TABLE `outcome_run_steps` (
	`id` text PRIMARY KEY NOT NULL,
	`run_id` text NOT NULL,
	`step_name` text NOT NULL,
	`node_type` text,
	`status` text DEFAULT 'pending' NOT NULL,
	`attempt` integer DEFAULT 0 NOT NULL,
	`input` text,
	`output` text,
	`error` text,
	`started_at` integer,
	`completed_at` integer,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (`run_id`) REFERENCES `outcome_runs`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `outcome_run_steps_run_id_idx` ON `outcome_run_steps` (`run_id`);--> statement-breakpoint
CREATE INDEX `outcome_run_steps_run_step_idx` ON `outcome_run_steps` (`run_id`,`step_name`);--> statement-breakpoint
CREATE INDEX `outcome_run_steps_status_idx` ON `outcome_run_steps` (`status`);--> statement-breakpoint
CREATE TABLE `outcome_runs` (
	`id` text PRIMARY KEY NOT NULL,
	`outcome_id` text NOT NULL,
	`status` text DEFAULT 'pending' NOT NULL,
	`user_id` text,
	`input` text,
	`output` text,
	`error` text,
	`started_at` integer,
	`completed_at` integer,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (`outcome_id`) REFERENCES `outcome_definitions`(`outcome_id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `outcome_runs_outcome_id_idx` ON `outcome_runs` (`outcome_id`);--> statement-breakpoint
CREATE INDEX `outcome_runs_status_idx` ON `outcome_runs` (`status`);--> statement-breakpoint
CREATE INDEX `outcome_runs_started_at_idx` ON `outcome_runs` (`started_at`);