package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	BindAddress       string
	LogLevel          string
	DataRoot          string
	DockerDataRoot    string
	BackupRoot        string
	SteamCMDPath      string
	DockerBinary      string
	GameImage         string
	SessionTTL        time.Duration
	SchedulerInterval time.Duration
	AllowedPortMin    int
	AllowedPortMax    int
	MetricsEnabled    bool
	SecretKey         []byte
	DevSeed           bool
	DevAdminEmail     string
	DevAdminPassword  string
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		BindAddress:       env("BIND_ADDRESS", ":8080"),
		LogLevel:          env("LOG_LEVEL", "info"),
		DataRoot:          env("DATA_ROOT", "/var/lib/game-control-plane/servers"),
		DockerDataRoot:    env("DOCKER_DATA_ROOT", env("DATA_ROOT", "/var/lib/game-control-plane/servers")),
		BackupRoot:        env("BACKUP_ROOT", "/var/lib/game-control-plane/backups"),
		SteamCMDPath:      env("STEAMCMD_PATH", "steamcmd"),
		DockerBinary:      env("DOCKER_BINARY", "docker"),
		GameImage:         env("GARRYSMOD_IMAGE", "game-server-control-plane/garrysmod:local"),
		SessionTTL:        durationEnv("SESSION_TTL", 24*time.Hour),
		SchedulerInterval: durationEnv("SCHEDULER_INTERVAL", 15*time.Second),
		AllowedPortMin:    intEnv("ALLOWED_PORT_MIN", 20000),
		AllowedPortMax:    intEnv("ALLOWED_PORT_MAX", 40000),
		MetricsEnabled:    boolEnv("METRICS_ENABLED", true),
		DevSeed:           boolEnv("DEV_SEED", false),
		DevAdminEmail:     os.Getenv("DEV_ADMIN_EMAIL"),
		DevAdminPassword:  os.Getenv("DEV_ADMIN_PASSWORD"),
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("SECRET_ENCRYPTION_KEY")))
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("SECRET_ENCRYPTION_KEY must be base64 for exactly 32 bytes")
	}
	cfg.SecretKey = key
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if !filepath.IsAbs(cfg.DataRoot) || !filepath.IsAbs(cfg.BackupRoot) || !filepath.IsAbs(cfg.DockerDataRoot) {
		return Config{}, errors.New("DATA_ROOT, BACKUP_ROOT, and DOCKER_DATA_ROOT must be absolute paths")
	}
	if cfg.AllowedPortMin < 1024 || cfg.AllowedPortMax > 65535 || cfg.AllowedPortMin > cfg.AllowedPortMax {
		return Config{}, errors.New("invalid allowed port range")
	}
	if cfg.SessionTTL <= 0 {
		return Config{}, errors.New("SESSION_TTL must be positive")
	}
	if cfg.SchedulerInterval < time.Second {
		return Config{}, errors.New("SCHEDULER_INTERVAL must be at least one second")
	}
	if cfg.DevSeed && (cfg.DevAdminEmail == "" || len(cfg.DevAdminPassword) < 12) {
		return Config{}, errors.New("DEV_SEED requires DEV_ADMIN_EMAIL and DEV_ADMIN_PASSWORD of at least 12 characters")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func intEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (c Config) ValidatePathRoot(path string) error {
	if !strings.HasPrefix(path, c.DataRoot+string(os.PathSeparator)) && path != c.DataRoot {
		return fmt.Errorf("path outside data root")
	}
	return nil
}
