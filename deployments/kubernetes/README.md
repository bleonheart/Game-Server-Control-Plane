# Kubernetes notes

Create `gscp-secrets` separately with `DATABASE_URL` and a base64-encoded 32-byte `SECRET_ENCRYPTION_KEY`. PostgreSQL is intentionally external; use a managed PostgreSQL service or a separately operated database with backups and TLS.

The current Docker runtime adapter requires access to a Docker-compatible node socket and a hostPath-backed shared server directory at `/var/lib/game-control-plane`. That makes these manifests suitable for controlled single-node or dedicated-node deployments, not a generic multi-node Kubernetes cluster. A future Kubernetes runtime adapter should replace the Docker socket and create game-server Pods directly.
