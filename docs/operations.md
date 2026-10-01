# Operations

## Configuration

Required settings are `DATABASE_URL` and `SECRET_ENCRYPTION_KEY`. Startup rejects an invalid encryption key, missing database URL, or invalid port range. All configuration is environment-based so containers do not require mutable config files.

## Backup and restore

Backups archive each server install/data directory as gzip-compressed tar, calculate a SHA-256 digest, and record metadata in PostgreSQL. Validation recomputes the stored checksum, and restore verifies that checksum before extraction. Restore is allowed only while a server is stopped or failed. Scheduled backups prune older files according to the per-server retention count. For large fleets, replace local storage with an S3-compatible `Storage` implementation and add off-host retention validation.

## Recovery

The worker reconciles every ten seconds. A desired-running server found stopped can restart automatically when `auto_recover` is true. Attempts are capped by `max_restart_attempts`; retry spacing grows exponentially up to five minutes. Operators can reset the restart count through the service layer when remediation is complete.

## Readiness and monitoring

`/health` confirms the HTTP process is alive. `/ready` requires PostgreSQL connectivity. `/metrics` emits Prometheus text format. `/api/v1/servers/{id}/status` exposes current runtime state, responsiveness, resource usage, uptime, restart counts, and update status to authenticated clients. Prometheus is available as an optional Compose profile.

## Database migrations

Run `make migrate` or `/usr/local/bin/migrate`. API and worker startup also apply pending migrations transactionally, so rolling out a compatible additive migration is idempotent across instances.
