package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

// BootstrapAdmin creates a single superadmin if no users exist. Idempotent —
// safe to call on every startup. Pulls credentials from
// SONAR_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}; both must be non-empty for it to
// take effect.
func BootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, email, password string) (created bool, err error) {
	if email == "" || password == "" {
		return false, nil
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, fmt.Errorf("bootstrap: count users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, fmt.Errorf("bootstrap: hash: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO users (email, display_name, password_hash, role, is_active)
		VALUES ($1, $2, $3, 'superadmin', TRUE)
	`, email, "Bootstrap Admin", hash); err != nil {
		return false, fmt.Errorf("bootstrap: insert user: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO sites (slug, name) VALUES ('default', 'Default Site')
		ON CONFLICT DO NOTHING
	`); err != nil {
		return false, fmt.Errorf("bootstrap: insert site: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ErrNoRows re-exports pgx.ErrNoRows so handlers don't have to import pgx
// just to detect "not found".
var ErrNoRows = errors.New("db: not found")

func mapPgxErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoRows
	}
	return err
}
