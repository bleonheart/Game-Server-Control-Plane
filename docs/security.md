# Security

## Authentication and secrets

Passwords use PBKDF2-HMAC-SHA-256 with random salts and 600,000 iterations. Login creates a random 256-bit bearer token; only its SHA-256 digest is stored in PostgreSQL. Logout deletes the corresponding session. Expired sessions are rejected and periodically removed.

RCON and game-server passwords are encrypted with AES-256-GCM before database insertion. `SECRET_ENCRYPTION_KEY` must decode to exactly 32 bytes and should be supplied from an external secret manager in production. It must not be committed to source control.

## Command execution

SteamCMD and Docker commands use direct argument vectors through `exec.CommandContext`; there is no shell interpolation. Garry's Mod map/gamemode/workshop values receive additional syntax validation. Secrets are written to a mode-0600 `server.cfg` instead of being passed as command-line arguments.

## Filesystem boundaries

Server paths are derived from generated IDs underneath `DATA_ROOT`. Deletion verifies root containment. Backup creation skips symlinks. Restore rejects archive paths that escape the requested destination and ignores unsupported link types.

## Authorization

Viewer can inspect servers, logs, backups, and schedules. Operator adds lifecycle, update, backup, restore, and schedule operations. Admin adds user/role changes and destructive server deletion. Audit entries capture actor, action, resource, result, request ID, timestamp, and useful metadata.

## Docker socket

Managed game containers are created with a read-only root filesystem, a writable bounded `/tmp`, dropped Linux capabilities, `no-new-privileges`, a PID limit, CPU/memory limits, and bounded Docker log rotation. The persistent game directory remains writable because SRCDS requires it.

Possession of the Docker socket is effectively host-level privilege. The local Compose deployment mounts the socket to demonstrate the runtime adapter. A production deployment should isolate the control plane on dedicated infrastructure, prefer rootless Docker or an authorization proxy, limit network reachability, and avoid sharing the socket with unrelated workloads. A Kubernetes-native runtime adapter is the preferred long-term architecture for multi-node clusters.
