package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/game-server-control-plane/internal/backups"
	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/example/game-server-control-plane/internal/servers"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db      *pgxpool.Pool
	servers *servers.Service
	backups *backups.Service
}

func NewService(db *pgxpool.Pool, serverService *servers.Service, backupService *backups.Service) *Service {
	return &Service{db: db, servers: serverService, backups: backupService}
}

func (s *Service) Create(ctx context.Context, serverID, kind string, intervalSeconds int) (model.Schedule, error) {
	if kind != "backup" && kind != "update" && kind != "restart" {
		return model.Schedule{}, fmt.Errorf("invalid schedule kind")
	}
	if intervalSeconds < 60 {
		return model.Schedule{}, fmt.Errorf("minimum interval is 60 seconds")
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM game_servers WHERE id=$1)`, serverID).Scan(&exists); err != nil {
		return model.Schedule{}, err
	}
	if !exists {
		return model.Schedule{}, servers.ErrNotFound
	}
	id, err := database.NewID("sch")
	if err != nil {
		return model.Schedule{}, err
	}
	var item model.Schedule
	if err := s.db.QueryRow(ctx, `INSERT INTO schedules(id,server_id,kind,interval_seconds,next_run_at) VALUES($1,$2,$3,$4,now()+make_interval(secs=>$4)) RETURNING id,server_id,kind,interval_seconds,enabled,next_run_at,created_at,updated_at`, id, serverID, kind, intervalSeconds).Scan(&item.ID, &item.ServerID, &item.Kind, &item.IntervalSeconds, &item.Enabled, &item.NextRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.Schedule{}, err
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, serverID string) ([]model.Schedule, error) {
	rows, err := s.db.Query(ctx, `SELECT id,server_id,kind,interval_seconds,enabled,next_run_at,created_at,updated_at FROM schedules WHERE server_id=$1 ORDER BY created_at`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.Schedule
	for rows.Next() {
		var item model.Schedule
		if err := rows.Scan(&item.ID, &item.ServerID, &item.Kind, &item.IntervalSeconds, &item.Enabled, &item.NextRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Delete(ctx context.Context, serverID, id string) error {
	result, err := s.db.Exec(ctx, `DELETE FROM schedules WHERE id=$1 AND server_id=$2`, id, serverID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	for i := 0; i < 20; i++ {
		item, ok, err := s.claimDue(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		var runErr error
		switch item.Kind {
		case "backup":
			_, runErr = s.backups.Create(ctx, item.ServerID)
			if runErr == nil {
				retention, err := s.servers.BackupRetentionCount(ctx, item.ServerID)
				if err != nil {
					runErr = err
				} else {
					runErr = s.backups.Prune(ctx, item.ServerID, retention)
				}
			}
		case "update":
			runErr = s.servers.Update(ctx, item.ServerID, false)
		case "restart":
			runErr = s.servers.Restart(ctx, item.ServerID)
		}
		if runErr != nil {
			eventID, idErr := database.NewID("evt")
			if idErr == nil {
				_, _ = s.db.Exec(ctx, `INSERT INTO server_events(id,server_id,kind,message,metadata) VALUES($1,$2,'schedule_failed',$3,'{}'::jsonb)`, eventID, item.ServerID, runErr.Error())
			}
		}
	}
	return nil
}

func (s *Service) claimDue(ctx context.Context) (model.Schedule, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return model.Schedule{}, false, err
	}
	defer tx.Rollback(ctx)
	var item model.Schedule
	err = tx.QueryRow(ctx, `SELECT id,server_id,kind,interval_seconds,enabled,next_run_at,created_at,updated_at FROM schedules WHERE enabled=true AND next_run_at <= now() ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&item.ID, &item.ServerID, &item.Kind, &item.IntervalSeconds, &item.Enabled, &item.NextRunAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Schedule{}, false, nil
	}
	if err != nil {
		return model.Schedule{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE schedules SET last_run_at=now(),next_run_at=now()+make_interval(secs=>$2),updated_at=now() WHERE id=$1`, item.ID, item.IntervalSeconds); err != nil {
		return model.Schedule{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Schedule{}, false, err
	}
	return item, true, nil
}
