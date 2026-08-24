CREATE TABLE `github_tokens` (
	`user_id` text PRIMARY KEY NOT NULL,
	`access_token` text NOT NULL,
	`username` text,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE cascade
);--> statement-breakpoint
CREATE INDEX `github_tokens_user_idx` ON `github_tokens` (`user_id`);