// Package migrations embeds and applies immutable SQL schema changes.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
)

//go:embed *.sql
var scripts embed.FS

// Apply serializes concurrent application starts and records a version only
// after its SQL has run successfully in the same transaction.
func Apply(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(832453029381)"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	files, err := fs.Glob(scripts, "*.sql")
	if err != nil {
		return err
	}
	for _, name := range files {
		var applied bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT FROM schema_migrations WHERE version=$1)", name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		script, err := scripts.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES ($1)", name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
