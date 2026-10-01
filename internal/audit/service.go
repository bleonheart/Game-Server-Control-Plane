package audit

import (
	"context"
	"encoding/json"

	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) Record(ctx context.Context, actorID, action, resource, result, requestID string, metadata map[string]any) error {
	id, err := database.NewID("aud")
	if err != nil {
		return err
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var actor any
	if actorID != "" {
		actor = actorID
	}
	_, err = s.db.Exec(ctx, `INSERT INTO audit_logs(id,actor_id,action,resource,result,metadata,request_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, actor, action, resource, result, body, requestID)
	return err
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]model.AuditRecord, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(ctx, `SELECT id,COALESCE(actor_id,''),action,resource,result,metadata,request_id,created_at FROM audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuditRecord, 0, limit)
	for rows.Next() {
		var item model.AuditRecord
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.Resource, &item.Result, &metadata, &item.RequestID, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
