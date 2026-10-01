package backups

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/model"
	controlruntime "github.com/example/game-server-control-plane/internal/runtime"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("backup not found")

type Service struct {
	db      *pgxpool.Pool
	storage Storage
	runtime controlruntime.Runtime
}

func NewService(db *pgxpool.Pool, storage Storage, runtimeService controlruntime.Runtime) *Service {
	return &Service{db: db, storage: storage, runtime: runtimeService}
}

func (s *Service) Create(ctx context.Context, serverID string) (model.Backup, error) {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+serverID)
	if err != nil {
		return model.Backup{}, err
	}
	defer release()
	var installPath string
	if err := s.db.QueryRow(ctx, `SELECT install_path FROM game_servers WHERE id=$1`, serverID).Scan(&installPath); err != nil {
		return model.Backup{}, err
	}
	id, err := database.NewID("bak")
	if err != nil {
		return model.Backup{}, err
	}
	location, size, sha, err := s.storage.Create(ctx, serverID, id, installPath)
	if err != nil {
		return model.Backup{}, err
	}
	var backup model.Backup
	if err := s.db.QueryRow(ctx, `INSERT INTO backups(id,server_id,location,size_bytes,sha256) VALUES($1,$2,$3,$4,$5) RETURNING id,server_id,location,size_bytes,sha256,created_at`, id, serverID, location, size, sha).Scan(&backup.ID, &backup.ServerID, &backup.Location, &backup.SizeBytes, &backup.SHA256, &backup.CreatedAt); err != nil {
		_ = s.storage.Delete(ctx, location)
		return model.Backup{}, err
	}
	return backup, nil
}

func (s *Service) List(ctx context.Context, serverID string, limit int) ([]model.Backup, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `SELECT id,server_id,location,size_bytes,sha256,created_at FROM backups WHERE server_id=$1 ORDER BY created_at DESC LIMIT $2`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Backup, 0, limit)
	for rows.Next() {
		var item model.Backup
		if err := rows.Scan(&item.ID, &item.ServerID, &item.Location, &item.SizeBytes, &item.SHA256, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Restore(ctx context.Context, serverID, backupID string) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+serverID)
	if err != nil {
		return err
	}
	defer release()
	var location, installPath, sha string
	var state model.ServerState
	if err := s.db.QueryRow(ctx, `SELECT b.location,g.install_path,g.actual_state,b.sha256 FROM backups b JOIN game_servers g ON g.id=b.server_id WHERE b.id=$1 AND b.server_id=$2`, backupID, serverID).Scan(&location, &installPath, &state, &sha); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if state != model.StateStopped && state != model.StateFailed {
		return fmt.Errorf("server must be stopped before restore")
	}
	runtimeStatus, err := s.runtime.Status(ctx, serverID)
	if err != nil {
		return err
	}
	if runtimeStatus.State == model.StateRunning {
		return fmt.Errorf("server must be stopped before restore")
	}
	if err := s.storage.Validate(ctx, location, sha); err != nil {
		return err
	}
	if err := os.MkdirAll(installPath, 0o750); err != nil {
		return err
	}
	return s.storage.Restore(ctx, location, installPath)
}

func (s *Service) Validate(ctx context.Context, serverID, backupID string) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+serverID)
	if err != nil {
		return err
	}
	defer release()
	var location, sha string
	if err := s.db.QueryRow(ctx, `SELECT location,sha256 FROM backups WHERE id=$1 AND server_id=$2`, backupID, serverID).Scan(&location, &sha); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return s.storage.Validate(ctx, location, sha)
}

func (s *Service) Prune(ctx context.Context, serverID string, keep int) error {
	if keep < 1 {
		return fmt.Errorf("retention must keep at least one backup")
	}
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+serverID)
	if err != nil {
		return err
	}
	defer release()
	rows, err := s.db.Query(ctx, `SELECT id,location FROM backups WHERE server_id=$1 ORDER BY created_at DESC OFFSET $2`, serverID, keep)
	if err != nil {
		return err
	}
	defer rows.Close()
	type doomed struct{ id, location string }
	var items []doomed
	for rows.Next() {
		var item doomed
		if err := rows.Scan(&item.id, &item.location); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if err := s.storage.Delete(ctx, item.location); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := s.db.Exec(ctx, `DELETE FROM backups WHERE id=$1`, item.id); err != nil {
			return err
		}
	}
	return nil
}
