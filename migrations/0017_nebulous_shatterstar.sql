CREATE TABLE `composition_snapshots` (
	`snapshot_id` text PRIMARY KEY NOT NULL,
	`lock_version` text NOT NULL,
	`system_ref` text NOT NULL,
	`system_digest` text NOT NULL,
	`compiler_version` text NOT NULL,
	`resolution_mode` text NOT NULL,
	`canonical_lock` text NOT NULL,
	`tenant_key` text NOT NULL,
	`closed_at` integer,
	`retention_class` text DEFAULT 'run' NOT NULL
);
--> statement-breakpoint
CREATE INDEX `composition_snapshots_tenant_idx` ON `composition_snapshots` (`tenant_key`);--> statement-breakpoint
CREATE INDEX `composition_snapshots_system_ref_idx` ON `composition_snapshots` (`system_ref`);--> statement-breakpoint
ALTER TABLE `workflow_instances` ADD `snapshot_id` text REFERENCES composition_snapshots(snapshot_id);--> statement-breakpoint
CREATE INDEX `workflow_instances_snapshot_id_idx` ON `workflow_instances` (`snapshot_id`);