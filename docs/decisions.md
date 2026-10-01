# Design decisions

## Opaque sessions instead of JWT

Opaque sessions make revocation, user disablement, and role changes immediately effective without a refresh-token subsystem. The database lookup cost is acceptable for a control-plane API and keeps authentication behavior explicit.

## Docker CLI behind an interface

The runtime interface is deliberately small. Calling the Docker CLI avoids coupling domain code to a large SDK while still using argument-safe process execution. The interface can later be implemented by the Docker Engine API, containerd, or Kubernetes.

## PostgreSQL locking for operation claims

Intermediate server states plus `SELECT ... FOR UPDATE` provide a simple concurrency invariant: one state-changing operation claims a server at a time. This is easier to reason about than process-local mutexes and works across multiple API instances.

## Fixed-interval schedules first

Persisted fixed intervals cover backups, updates, and periodic restarts without pulling a cron parser into the core. Cron expressions and maintenance windows are a roadmap item once durable operation records are introduced.
