package model

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type ServerState string

const (
	StateProvisioning ServerState = "provisioning"
	StateStopped      ServerState = "stopped"
	StateStarting     ServerState = "starting"
	StateRunning      ServerState = "running"
	StateStopping     ServerState = "stopping"
	StateUpdating     ServerState = "updating"
	StateFailed       ServerState = "failed"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Roles        []Role    `json:"roles"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Principal struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Roles  []Role `json:"roles"`
}

func (p Principal) HasRole(role Role) bool {
	for _, v := range p.Roles {
		if v == role {
			return true
		}
	}
	return false
}

func (p Principal) CanOperate() bool {
	return p.HasRole(RoleAdmin) || p.HasRole(RoleOperator)
}

type GameServer struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Game                 string            `json:"game"`
	DesiredState         ServerState       `json:"desired_state"`
	ActualState          ServerState       `json:"actual_state"`
	Image                string            `json:"image"`
	InstallPath          string            `json:"install_path"`
	Map                  string            `json:"map"`
	Gamemode             string            `json:"gamemode"`
	MaxPlayers           int               `json:"max_players"`
	Hostname             string            `json:"hostname"`
	WorkshopCollection   string            `json:"workshop_collection,omitempty"`
	RCONPassword         string            `json:"-"`
	ServerPassword       string            `json:"-"`
	Environment          map[string]string `json:"environment"`
	ExtraArgs            []string          `json:"extra_args"`
	CPUQuota             int64             `json:"cpu_quota"`
	MemoryBytes          int64             `json:"memory_bytes"`
	AutoRecover          bool              `json:"auto_recover"`
	MaxRestartAttempts   int               `json:"max_restart_attempts"`
	BackupRetentionCount int               `json:"backup_retention_count"`
	RestartCount         int               `json:"restart_count"`
	LastError            string            `json:"last_error,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type ServerPort struct {
	ID         string `json:"id"`
	ServerID   string `json:"server_id"`
	Name       string `json:"name"`
	HostPort   int    `json:"host_port"`
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`
}

type RuntimeStatus struct {
	State        ServerState   `json:"state"`
	CPUPercent   float64       `json:"cpu_percent"`
	MemoryBytes  int64         `json:"memory_bytes"`
	Uptime       time.Duration `json:"uptime"`
	RestartCount int           `json:"restart_count"`
	Healthy      bool          `json:"healthy"`
}

type ServerStatus struct {
	DesiredState         ServerState `json:"desired_state"`
	RecordedState        ServerState `json:"recorded_state"`
	RuntimeState         ServerState `json:"runtime_state"`
	Responsive           bool        `json:"responsive"`
	CPUPercent           float64     `json:"cpu_percent"`
	MemoryBytes          int64       `json:"memory_bytes"`
	UptimeSeconds        float64     `json:"uptime_seconds"`
	RuntimeRestartCount  int         `json:"runtime_restart_count"`
	RecoveryRestartCount int         `json:"recovery_restart_count"`
	UpdateStatus         string      `json:"update_status"`
}

type UpdateHistory struct {
	ID         string     `json:"id"`
	ServerID   string     `json:"server_id"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Backup struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Location  string    `json:"location"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

type Schedule struct {
	ID              string    `json:"id"`
	ServerID        string    `json:"server_id"`
	Kind            string    `json:"kind"`
	IntervalSeconds int       `json:"interval_seconds"`
	Enabled         bool      `json:"enabled"`
	NextRunAt       time.Time `json:"next_run_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AuditRecord struct {
	ID        string         `json:"id"`
	ActorID   string         `json:"actor_id,omitempty"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Result    string         `json:"result"`
	Metadata  map[string]any `json:"metadata"`
	RequestID string         `json:"request_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}
