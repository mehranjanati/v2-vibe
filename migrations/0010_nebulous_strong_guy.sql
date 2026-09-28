CREATE TABLE `workflow_dags` (
	`workflow_id` text PRIMARY KEY NOT NULL,
	`schema_version` integer DEFAULT 2 NOT NULL,
	`dag_json` text NOT NULL,
	`status` text DEFAULT 'generated' NOT NULL,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP
);
--> statement-breakpoint
CREATE INDEX `workflow_dags_status_idx` ON `workflow_dags` (`status`);--> statement-breakpoint
CREATE INDEX `workflow_dags_updated_at_idx` ON `workflow_dags` (`updated_at`);--> statement-breakpoint
CREATE TABLE `workflow_instances` (
	`id` text PRIMARY KEY NOT NULL,
	`workflow_id` text NOT NULL,
	`status` text DEFAULT 'pending' NOT NULL,
	`input` text,
	`output` text,
	`error` text,
	`started_at` integer,
	`completed_at` integer,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (`workflow_id`) REFERENCES `workflow_dags`(`workflow_id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `workflow_instances_workflow_id_idx` ON `workflow_instances` (`workflow_id`);--> statement-breakpoint
CREATE INDEX `workflow_instances_status_idx` ON `workflow_instances` (`status`);--> statement-breakpoint
CREATE INDEX `workflow_instances_started_at_idx` ON `workflow_instances` (`started_at`);--> statement-breakpoint
CREATE TABLE `workflow_step_logs` (
	`id` text PRIMARY KEY NOT NULL,
	`instance_id` text NOT NULL,
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
	FOREIGN KEY (`instance_id`) REFERENCES `workflow_instances`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `workflow_step_logs_instance_id_idx` ON `workflow_step_logs` (`instance_id`);--> statement-breakpoint
CREATE INDEX `workflow_step_logs_instance_step_idx` ON `workflow_step_logs` (`instance_id`,`step_name`);--> statement-breakpoint
CREATE INDEX `workflow_step_logs_status_idx` ON `workflow_step_logs` (`status`);