CREATE TABLE `github_mirrors` (
	`user_id` text NOT NULL,
	`agent_id` text NOT NULL,
	`repo_full_name` text NOT NULL,
	`branch` text DEFAULT 'main' NOT NULL,
	`last_pushed_sha` text,
	`last_generation_id` text,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(`user_id`, `agent_id`)
);
