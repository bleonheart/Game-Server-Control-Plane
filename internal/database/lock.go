package database

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

func AcquireResourceLock(ctx context.Context, pool *pgxpool.Pool, key string) (func(), error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = tx.Rollback(context.Background())
		})
	}, nil
}
