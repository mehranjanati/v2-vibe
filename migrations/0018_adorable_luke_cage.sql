CREATE TABLE `memory_records` (
	`id` text PRIMARY KEY NOT NULL,
	`tenant_key` text NOT NULL,
	`scope` text NOT NULL,
	`subject` text NOT NULL,
	`kind` text NOT NULL,
	`key` text,
	`text` text,
	`data` text,
	`source` text NOT NULL,
	`producer` text NOT NULL,
	`version` text NOT NULL,
	`created_at` integer NOT NULL,
	`expires_at` integer
);
--> statement-breakpoint
CREATE INDEX `memory_records_recall_idx` ON `memory_records` (`tenant_key`,`scope`,`subject`,`created_at`);--> statement-breakpoint
CREATE INDEX `memory_records_expiry_idx` ON `memory_records` (`tenant_key`,`expires_at`);--> statement-breakpoint
CREATE INDEX `memory_records_key_idx` ON `memory_records` (`tenant_key`,`scope`,`subject`,`kind`,`key`);