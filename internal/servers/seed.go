package servers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/example/game-server-control-plane/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Service) SeedExample(ctx context.Context, seedSecret string) error {
	var id string
	err := s.db.QueryRow(ctx, `SELECT id FROM game_servers WHERE name='demo-gmod'`).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	hash := sha256.Sum256([]byte(seedSecret + ":demo-gmod-rcon"))
	_, err = s.Create(ctx, CreateInput{
		Name: "demo-gmod", Game: "garrysmod", Map: "gm_construct", Gamemode: "sandbox", MaxPlayers: 16,
		Hostname: "Game Server Control Plane Demo", RCONPassword: hex.EncodeToString(hash[:12]), Environment: map[string]string{},
		CPUQuota: 100000, MemoryBytes: 1 << 30, AutoRecover: true, MaxRestartAttempts: 5, BackupRetentionCount: 7,
		Ports: []model.ServerPort{{Name: "game", HostPort: 27015, TargetPort: 27015, Protocol: "udp"}, {Name: "game-tcp", HostPort: 27015, TargetPort: 27015, Protocol: "tcp"}},
	})
	return err
}
