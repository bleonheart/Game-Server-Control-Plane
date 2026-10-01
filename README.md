<h1 align="center">Game Server Control Plane</h1>

<p align="center">
  Production-style control plane for provisioning, operating, updating, monitoring, backing up, and recovering dedicated game servers.
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-Control_Plane-4169E1?logo=postgresql&logoColor=white">
  <img alt="Docker" src="https://img.shields.io/badge/Docker-Runtime-2496ED?logo=docker&logoColor=white">
  <img alt="Garry's Mod" src="https://img.shields.io/badge/Garry's_Mod-SRCDS-1F6FEB?logo=steam&logoColor=white">
  <img alt="Prometheus" src="https://img.shields.io/badge/Prometheus-Metrics-E6522C?logo=prometheus&logoColor=white">
  <img alt="GitHub Actions" src="https://img.shields.io/badge/GitHub_Actions-CI-2088FF?logo=githubactions&logoColor=white">
</p>

## Overview

Game Server Control Plane is a production-oriented backend and operations project for managing dedicated game servers through one API and worker system. The first adapter targets Garry's Mod / SRCDS, while game-specific behavior is isolated behind interfaces so additional SteamCMD, Source, or non-Source servers can be added without rewriting the control plane.

The repository demonstrates backend engineering, systems administration, networking, Docker orchestration, PostgreSQL concurrency control, SteamCMD automation, observability, backups, recovery, scheduling, security, and CI/CD as one connected system rather than a collection of isolated examples.

## Quick Start

Requirements: Go 1.23+, Docker with the Compose plugin, `make`, and enough disk space for a Garry's Mod dedicated server installation.

```bash
git clone <your-repository-url>
cd game-server-control-plane
cp .env.example .env
openssl rand -base64 32
```

Set the generated value as `SECRET_ENCRYPTION_KEY`, choose a strong PostgreSQL password, then start the environment:

```bash
make dev
```

The API is exposed at:

```text
http://localhost:8080
```

Prometheus can be enabled through the Compose observability profile and is exposed on port `9090`.

Useful commands:

```bash
make dev
make test
make lint
make migrate
make build
```

For development seed data, set `DEV_SEED=true`, `DEV_ADMIN_EMAIL`, and a `DEV_ADMIN_PASSWORD` of at least 12 characters. Seed mode creates an administrator account and one `demo-gmod` configuration. Provisioning is real, so the first seed downloads and validates Garry's Mod through SteamCMD.

## Key Capabilities

| Area | Demonstrated implementation |
| --- | --- |
| Authentication | Opaque bearer sessions, SHA-256 token digests, expiry, revocation, PBKDF2-HMAC-SHA-256 passwords |
| Authorization | Admin, operator, and viewer RBAC enforced on protected operations |
| Game adapters | Garry's Mod / SRCDS adapter with validation, startup generation, `server.cfg`, map, gamemode, slots, passwords, workshop configuration |
| Lifecycle | Provision, start, stop, force-stop, restart, update, validate, delete, desired/actual state reconciliation |
| Runtime | Docker isolation, CPU/memory/PID limits, dropped capabilities, read-only root filesystem, mapped ports, health state, capped logs |
| SteamCMD | Install, update, validate, failure detection, captured output, persisted update history and progress |
| Concurrency | Transactional state claims, PostgreSQL advisory locks, conflict rejection, multi-instance-safe scheduled job claiming |
| Recovery | Crash detection, bounded restart attempts, exponential backoff, unhealthy state, recovery audit events |
| Logs | Recent Docker logs and live Server-Sent Events streaming with bounded output |
| Backups | Manual/scheduled backups, SHA-256 verification, restore protection, retention pruning, storage abstraction |
| Scheduling | Persistent backup, update, and restart schedules with duplicate-execution prevention |
| Observability | Structured JSON logs, request IDs, health/readiness endpoints, Prometheus-compatible fleet and server metrics |
| Security | Encrypted server secrets, command-injection prevention, path containment, port restrictions, secret-safe API errors |
| Delivery | SQL migrations, Docker Compose, optional Kubernetes manifests, GitHub Actions, unit and integration tests |

## Server Lifecycle and Concurrency

Each managed server stores both `desired_state` and `actual_state`.

Lifecycle, update, delete, validate, backup, and restore operations acquire a server-scoped PostgreSQL advisory lock so separate API and worker instances cannot perform conflicting long-running work on the same server. Lifecycle state claims also take a short row lock and move the server into an intermediate state before Docker is touched.

A second incompatible request therefore receives HTTP `409` instead of racing the first operation.

The worker periodically inspects Docker and reconciles the database with actual runtime state. If a desired-running server stops unexpectedly and automatic recovery is enabled, the worker retries with bounded exponential backoff and records recovery events.

Persistent schedules are claimed with `FOR UPDATE SKIP LOCKED`. A worker advances the next-run timestamp in the same transaction before executing the job, allowing multiple workers to poll the schedule table without duplicate claims.

## API Examples

### Authenticate

```bash
curl -sS http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.test","password":"your-password"}'
```

### Create a Garry's Mod server

```bash
curl -sS http://localhost:8080/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"gmod-01",
    "game":"garrysmod",
    "map":"gm_construct",
    "gamemode":"sandbox",
    "max_players":24,
    "hostname":"Portfolio GMod",
    "rcon_password":"replace-with-long-rcon-password",
    "auto_recover":true,
    "ports":[
      {"name":"game","host_port":27015,"target_port":27015,"protocol":"udp"},
      {"name":"game-tcp","host_port":27015,"target_port":27015,"protocol":"tcp"}
    ]
  }'
```

### Start the server

```bash
curl -sS -X POST http://localhost:8080/api/v1/servers/$SERVER_ID/start \
  -H "Authorization: Bearer $TOKEN"
```

### Stream live logs

```bash
curl -N "http://localhost:8080/api/v1/servers/$SERVER_ID/logs?follow=true&tail=200" \
  -H "Authorization: Bearer $TOKEN"
```

### Inspect runtime health

```bash
curl -sS http://localhost:8080/api/v1/servers/$SERVER_ID/status \
  -H "Authorization: Bearer $TOKEN"
```

### Schedule daily backups

```bash
curl -sS http://localhost:8080/api/v1/servers/$SERVER_ID/schedules \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"backup","interval_seconds":86400}'
```

## Security Model

- Passwords use salted PBKDF2-HMAC-SHA-256 hashes.
- Session tokens are random 256-bit values; only SHA-256 digests are stored.
- RCON and server passwords are encrypted at rest with AES-256-GCM using `SECRET_ENCRYPTION_KEY`.
- SteamCMD and Docker are invoked through argument arrays rather than shell-concatenated commands.
- Game containers use a read-only root filesystem, dropped Linux capabilities, `no-new-privileges`, PID limits, bounded Docker logs, and explicit CPU/memory limits.
- Game names, map/gamemode tokens, ports, paths, configuration values, request bodies, and role changes are validated.
- Backup extraction verifies that archive paths remain inside the intended destination.
- Server deletion and local backup operations verify filesystem root containment.
- API error responses avoid leaking internal errors and secrets.
- The allowed host-port range is centrally configurable.

The Docker socket is a privileged capability. Compose mounts it because the current runtime adapter controls Docker directly. Production deployments should use a dedicated host, rootless Docker or an authorization/socket proxy, and network controls around the control-plane services.

See [Security](docs/security.md) and [Security Policy](SECURITY.md).

## Observability and Recovery

The API emits structured JSON through `log/slog`, propagates or generates `X-Request-ID`, and exposes:

```text
GET /health
GET /ready
GET /metrics
```

Metrics cover fleet totals, running servers, restart/update counters, update failures, per-server CPU, memory, uptime, and responsiveness.

`GET /api/v1/servers/{id}/status` reports Docker state, Source-query responsiveness, resource usage, uptime, recovery count, and latest update status. Runtime reconciliation performs the same Source A2S UDP responsiveness probe for running Garry's Mod servers.

Automatic recovery uses bounded retries and exponential backoff to avoid infinite restart loops. Recovery actions are recorded as server events and audit data.

## Backup and Recovery

Backups include persistent game data and relevant server configuration. The local implementation is behind a storage interface so an S3-compatible backend can replace it without changing the control-plane workflow.

The implementation includes:

- manual and scheduled backups;
- metadata persisted in PostgreSQL;
- SHA-256 validation before restore;
- archive path traversal protection;
- executable-bit preservation;
- retention pruning;
- guarded restore operations;
- operation serialization against conflicting server work.

## Database Design

The initial migration creates:

- users and roles;
- sessions;
- game servers and ports;
- server events;
- update history;
- backups;
- schedules;
- audit logs.

The schema uses foreign keys, uniqueness constraints, validation checks, timestamps, indexes, and transactional migrations. Secrets are encrypted before insertion. Applied migrations are tracked in `schema_migrations`.

## CI/CD

GitHub Actions validates the repository on pushes and pull requests.

The pipeline performs:

1. Go formatting checks;
2. `go vet` static analysis;
3. race-enabled tests;
4. coverage generation;
5. `govulncheck` security scanning;
6. API, worker, and migration binary builds;
7. production Docker image build.

Integration coverage uses PostgreSQL plus fake runtime and SteamCMD implementations to verify control-plane behavior without requiring real game containers.

## Testing

```bash
make test
make lint
make build
```

Unit tests cover authentication primitives, Garry's Mod validation/config generation, backup round trips and tamper detection, executable restoration, Docker stats parsing, SteamCMD progress buffering, and authorization middleware.

The integration suite uses a real PostgreSQL database with fake SteamCMD and Docker implementations to verify concurrency behavior such as two simultaneous starts not both executing.

```bash
TEST_DATABASE_URL='postgres://gscp:password@localhost:5432/gscp_test?sslmode=disable' make test-integration
```

## Repository Structure

```text
game-server-control-plane/
├── .github/workflows/
├── cmd/
│   ├── api/
│   ├── migrate/
│   └── worker/
├── internal/
│   ├── api/
│   ├── app/
│   ├── audit/
│   ├── auth/
│   ├── backups/
│   ├── config/
│   ├── database/
│   ├── games/
│   │   └── garrysmod/
│   ├── model/
│   ├── observability/
│   ├── runtime/
│   ├── scheduler/
│   ├── secrets/
│   ├── servers/
│   └── steamcmd/
├── migrations/
├── deployments/
│   ├── docker/
│   └── kubernetes/
├── docs/
├── tests/
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── README.md
```

## Deployment

`docker-compose.yml` is the supported local and development deployment path. PostgreSQL uses persistent storage, while game files and backups are bind-mounted under `./data`. `DOCKER_DATA_ROOT` supplies the matching host-side path to sibling game containers.

`deployments/kubernetes/` demonstrates configuration, probes, persistent storage, Service/Ingress resources, and external secret/database expectations. The current Docker runtime adapter means the Kubernetes example is intentionally scoped to controlled Docker-compatible nodes rather than presented as a generic multi-node game-server runtime.

## Documentation

| Document | Focus |
| --- | --- |
| [Architecture](docs/architecture.md) | Components, data flow, reconciliation, locking, runtime boundaries |
| [Operations](docs/operations.md) | Local operation, lifecycle workflows, backups, schedules, recovery |
| [Security](docs/security.md) | Authentication, secret handling, Docker risk, path and command safety |
| [Design Decisions](docs/decisions.md) | Important implementation choices and tradeoffs |
| [Security Policy](SECURITY.md) | Reporting and security expectations |
| [Kubernetes Notes](deployments/kubernetes/README.md) | Scope and assumptions of the optional Kubernetes deployment |

## Security and Limitations

- Lifecycle API calls are synchronous; long SteamCMD updates occupy the initiating request. A durable operation queue would improve UX and horizontal scalability.
- The current backup backend is local filesystem storage. The abstraction is designed for a future S3-compatible implementation.
- The scheduler supports fixed intervals rather than cron expressions or maintenance windows.
- Metrics are in-memory and reset when the process restarts; durable operational history remains in PostgreSQL.
- The Docker runtime requires Docker socket access and shared filesystem visibility.
- Kubernetes manifests demonstrate deployment shape but do not replace the Docker-specific runtime adapter.
- Garry's Mod is the only game adapter currently implemented.
- The project does not claim production uptime, scale, or benchmark results that are not generated from an actual deployment.

## License

MIT. See [LICENSE](LICENSE).
