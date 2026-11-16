CREATE TABLE `git_objects` (
	`app_id` text NOT NULL,
	`sha` text NOT NULL,
	`kind` text NOT NULL,
	`content` text NOT NULL,
	`size` integer NOT NULL,
	`created_at` integer DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(`app_id`, `sha`)
);
--> statement-breakpoint
CREATE INDEX `git_objects_kind_idx` ON `git_objects` (`app_id`,`kind`);--> statement-breakpoint
CREATE TABLE `git_refs` (
	`app_id` text NOT NULL,
	`name` text NOT NULL,
	`sha` text NOT NULL,
	`updated_at` integer DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(`app_id`, `name`)
);
