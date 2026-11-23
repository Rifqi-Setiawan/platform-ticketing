// Package expiry drains persisted overdue holds at startup and on every tick.
package expiry

import (
	"context"
	"log/slog"
	"time"
)

type Store interface {
	ExpireNext(context.Context) (bool, error)
	PurgeExpiredSession(context.Context) (bool, error)
}

func Run(ctx context.Context, store Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		drain(ctx, store, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func drain(ctx context.Context, store Store, logger *slog.Logger) {
	batch, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for i := 0; i < 50 && batch.Err() == nil; i++ {
		found, err := store.ExpireNext(batch)
		if err != nil {
			if ctx.Err() == nil {
				logger.Warn("expiry scan failed", "error", err)
			}
			return
		}
		if !found {
			break
		}
	}
	if batch.Err() == nil {
		if _, err := store.PurgeExpiredSession(batch); err != nil && ctx.Err() == nil {
			logger.Warn("session cleanup failed", "error", err)
		}
	}
}
