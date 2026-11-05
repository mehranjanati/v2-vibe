ALTER TABLE `workflow_dags` ADD `user_id` text;--> statement-breakpoint
CREATE INDEX `workflow_dags_user_idx` ON `workflow_dags` (`user_id`);--> statement-breakpoint
ALTER TABLE `workflow_instances` ADD `user_id` text;