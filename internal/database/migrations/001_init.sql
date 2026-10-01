CREATE TABLE users (
    id text PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    name text PRIMARY KEY CHECK (name IN ('admin', 'operator', 'viewer'))
);

INSERT INTO roles(name) VALUES ('admin'), ('operator'), ('viewer') ON CONFLICT DO NOTHING;

CREATE TABLE user_roles (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_name text NOT NULL REFERENCES roles(name) ON DELETE RESTRICT,
    PRIMARY KEY(user_id, role_name)
);

CREATE TABLE sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE game_servers (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    game text NOT NULL,
    desired_state text NOT NULL CHECK (desired_state IN ('provisioning','stopped','starting','running','stopping','updating','failed')),
    actual_state text NOT NULL CHECK (actual_state IN ('provisioning','stopped','starting','running','stopping','updating','failed')),
    image text NOT NULL,
    install_path text NOT NULL UNIQUE,
    map text NOT NULL,
    gamemode text NOT NULL,
    max_players integer NOT NULL CHECK (max_players BETWEEN 1 AND 128),
    hostname text NOT NULL,
    workshop_collection text NOT NULL DEFAULT '',
    rcon_password_enc bytea,
    server_password_enc bytea,
    environment jsonb NOT NULL DEFAULT '{}'::jsonb,
    extra_args jsonb NOT NULL DEFAULT '[]'::jsonb,
    cpu_quota bigint NOT NULL DEFAULT 100000 CHECK (cpu_quota BETWEEN 10000 AND 6400000),
    memory_bytes bigint NOT NULL DEFAULT 1073741824 CHECK (memory_bytes BETWEEN 134217728 AND 137438953472),
    auto_recover boolean NOT NULL DEFAULT true,
    max_restart_attempts integer NOT NULL DEFAULT 5 CHECK (max_restart_attempts BETWEEN 0 AND 100),
    backup_retention_count integer NOT NULL DEFAULT 7 CHECK (backup_retention_count BETWEEN 1 AND 1000),
    restart_count integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_servers_state_idx ON game_servers(actual_state);
CREATE INDEX game_servers_game_idx ON game_servers(game);

CREATE TABLE server_ports (
    id text PRIMARY KEY,
    server_id text NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
    name text NOT NULL,
    host_port integer NOT NULL CHECK (host_port BETWEEN 1 AND 65535),
    target_port integer NOT NULL CHECK (target_port BETWEEN 1 AND 65535),
    protocol text NOT NULL CHECK (protocol IN ('tcp','udp')),
    UNIQUE(host_port, protocol),
    UNIQUE(server_id, name)
);

CREATE TABLE server_events (
    id text PRIMARY KEY,
    server_id text NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
    kind text NOT NULL,
    message text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX server_events_server_created_idx ON server_events(server_id, created_at DESC);

CREATE TABLE update_history (
    id text PRIMARY KEY,
    server_id text NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
    status text NOT NULL,
    output text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

CREATE INDEX update_history_server_started_idx ON update_history(server_id, started_at DESC);

CREATE TABLE backups (
    id text PRIMARY KEY,
    server_id text NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
    location text NOT NULL UNIQUE,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    sha256 text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX backups_server_created_idx ON backups(server_id, created_at DESC);

CREATE TABLE schedules (
    id text PRIMARY KEY,
    server_id text NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('backup','update','restart')),
    interval_seconds integer NOT NULL CHECK (interval_seconds >= 60),
    enabled boolean NOT NULL DEFAULT true,
    next_run_at timestamptz NOT NULL,
    last_run_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX schedules_due_idx ON schedules(enabled, next_run_at);

CREATE TABLE audit_logs (
    id text PRIMARY KEY,
    actor_id text REFERENCES users(id) ON DELETE SET NULL,
    action text NOT NULL,
    resource text NOT NULL,
    result text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    request_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_created_idx ON audit_logs(created_at DESC);
CREATE INDEX audit_logs_actor_idx ON audit_logs(actor_id, created_at DESC);
