# Security Policy

## Reporting

Do not publish suspected vulnerabilities as public issues. Report them privately to the repository maintainer with a concise reproduction, affected version or commit, impact, and any known mitigations.

## Security-sensitive surfaces

The highest-risk surfaces are Docker socket access, SteamCMD execution, filesystem restore, server command-line configuration, bearer sessions, encrypted game-server secrets, and host-port allocation. Changes in these areas should receive focused review and tests.

## Deployment expectations

Use unique production secrets, restrict control-plane network access, keep PostgreSQL private, protect the Docker socket with dedicated infrastructure or an authorization proxy, run current container/runtime versions, and review dependency and image vulnerability reports from CI.
