# Architecture

## Responsibilities

The API is a thin transport layer over domain services. Authentication, lifecycle, SteamCMD, runtime, backup, scheduling, audit, and observability concerns live in separate packages. Constructors inject concrete dependencies; there is no mutable package-global application state.

`games.Adapter` owns game-specific validation, configuration generation, startup arguments, Steam App ID selection, and health endpoint interpretation. `runtime.Runtime` owns container/process operations. `steamcmd.Client` owns installation/update/validation. `backups.Storage` owns backup persistence. This separation keeps the control plane independent of Garry's Mod and makes external behavior mockable.

## Lifecycle

Provisioning creates durable metadata first with `actual_state=provisioning`, installs the game through SteamCMD, writes adapter configuration, creates the Docker container, then records `stopped`. A failure leaves the record in `failed` with a bounded diagnostic message so operators have evidence instead of a silently rolled-back external side effect.

Lifecycle, update, validation, delete, backup, and restore operations take a server-scoped PostgreSQL transaction advisory lock for the duration of the external work. This serializes conflicting work across API and worker processes without process-local mutexes. Lifecycle state claims additionally use a short row lock to move the server to an intermediate state before touching Docker. The row lock keeps state transitions deterministic while the advisory lock protects the full external operation.

The worker treats Docker state as authoritative for process existence and periodically reconciles it with PostgreSQL. Fresh transitional states receive a grace period so reconciliation does not overwrite in-flight work. Desired-running servers that unexpectedly stop are eligible for bounded restart recovery with exponential backoff. Live status also combines Docker stats with a Source A2S UDP probe for Garry's Mod responsiveness.

## Scheduling

Schedules are fixed intervals stored in PostgreSQL. Workers select due rows with `FOR UPDATE SKIP LOCKED`, update `last_run_at` and `next_run_at`, commit, then execute. This prevents simultaneous workers from claiming the same due schedule. The tradeoff is at-most-once claiming if a worker dies after committing but before execution; a durable operations table is the planned extension for exactly tracked execution attempts.

## Data ownership

PostgreSQL stores control-plane metadata and operational history. Server files live beneath `DATA_ROOT`. Backups live behind the `backups.Storage` abstraction. Docker owns live process/container state. No component assumes a database `running` value alone proves a process is running.
