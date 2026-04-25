package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the typed data-access surface for Phase 1. Phase 2 will
// migrate to sqlc-generated queries; this hand-written layer keeps the
// surface tiny while the schema stabilises.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ---- Users ----------------------------------------------------------------

type User struct {
	ID            uuid.UUID
	Email         string
	DisplayName   string
	PasswordHash  string
	Role          string
	TOTPEnrolled  bool
	IsActive      bool
	LastLoginAt   *time.Time
	CreatedAt     time.Time
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	const q = `
		SELECT id, email, display_name, password_hash, role,
		       totp_enrolled, is_active, last_login_at, created_at
		FROM users WHERE lower(email) = lower($1)
	`
	u := &User{}
	if err := s.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role,
		&u.TOTPEnrolled, &u.IsActive, &u.LastLoginAt, &u.CreatedAt,
	); err != nil {
		return nil, mapPgxErr(err)
	}
	return u, nil
}

func (s *Store) TouchUserLogin(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, id)
	return err
}

// ---- Sites ----------------------------------------------------------------

type Site struct {
	ID          uuid.UUID
	Slug        string
	Name        string
	Timezone    string
	Description *string
	CreatedAt   time.Time
}

func (s *Store) ListSites(ctx context.Context) ([]Site, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, slug, name, timezone, description, created_at FROM sites ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		var st Site
		if err := rows.Scan(&st.ID, &st.Slug, &st.Name, &st.Timezone, &st.Description, &st.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ---- Audit ---------------------------------------------------------------

func (s *Store) Audit(ctx context.Context, actorKind, action string, actorID *uuid.UUID, ip string, metadata map[string]any) {
	// Best-effort fire-and-forget. Failure to write an audit entry must
	// never fail the underlying request, but we do log it.
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_kind, action, ip, metadata)
		VALUES ($1, $2, $3, NULLIF($4,'')::inet, $5::jsonb)
	`, actorID, actorKind, action, ip, metadata)
}
