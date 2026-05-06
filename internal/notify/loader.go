package notify

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
)

// Channel is a resolved notification_channels row ready for dispatch.
type Channel struct {
	ID            uuid.UUID
	Kind          string
	Name          string
	Config        map[string]any
	SigningSecret []byte // plaintext; webhook HMAC secret, optional
}

// LoadSMTP reads smtp_settings id=1 and decrypts the password.
func LoadSMTP(ctx context.Context, pool *pgxpool.Pool, sealer *crypto.Sealer) (SMTPConfig, error) {
	var cfg SMTPConfig
	var encPass []byte
	err := pool.QueryRow(ctx, `
		SELECT host, port, COALESCE("user",''), enc_password, COALESCE(from_addr,''), use_tls
		  FROM smtp_settings WHERE id = 1`).
		Scan(&cfg.Host, &cfg.Port, &cfg.User, &encPass, &cfg.From, &cfg.UseTLS)
	if err != nil {
		return cfg, fmt.Errorf("notify: load smtp_settings: %w", err)
	}
	if len(encPass) > 0 && sealer != nil {
		b, oerr := sealer.Open(encPass, []byte("smtp:password"))
		if oerr == nil {
			cfg.Pass = string(b)
		}
	}
	return cfg, nil
}

// LoadChannels fetches active notification rows for the given ids (order preserved).
func LoadChannels(ctx context.Context, pool *pgxpool.Pool, sealer *crypto.Sealer, ids []uuid.UUID) ([]Channel, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id, kind, name, config, enc_secret
		  FROM notification_channels
		 WHERE id = ANY($1::uuid[]) AND is_active
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("notify: load channels: %w", err)
	}
	defer rows.Close()

	byID := make(map[uuid.UUID]Channel, len(ids))
	for rows.Next() {
		var ch Channel
		var cfgBytes []byte
		var encSecret []byte
		if err := rows.Scan(&ch.ID, &ch.Kind, &ch.Name, &cfgBytes, &encSecret); err != nil {
			return nil, err
		}
		if len(cfgBytes) > 0 {
			_ = json.Unmarshal(cfgBytes, &ch.Config)
		}
		if ch.Config == nil {
			ch.Config = map[string]any{}
		}
		if len(encSecret) > 0 && sealer != nil {
			ad := []byte("notification:" + ch.ID.String())
			plain, oerr := sealer.Open(encSecret, ad)
			if oerr == nil {
				ch.SigningSecret = plain
			}
		}
		byID[ch.ID] = ch
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Channel, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
