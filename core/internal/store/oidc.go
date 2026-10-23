package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// OIDCConfig is the single row of oidc_config. ClientSecretEnc holds the
// encrypted client secret; a nil value keeps the stored one when saving.
type OIDCConfig struct {
	Enabled         bool
	Issuer          string
	ClientID        string
	ClientSecretEnc []byte
	ButtonLabel     string
	UpdatedAt       time.Time
	UpdatedBy       string
}

const oidcConfigColumns = `enabled, issuer, client_id, client_secret_enc, button_label,
	updated_at, updated_by`

// GetOIDCConfig returns the OIDC configuration. A database without a row
// returns the zero configuration instead of an error.
func (s *Store) GetOIDCConfig(ctx context.Context) (OIDCConfig, error) {
	var config OIDCConfig
	err := s.pool.QueryRow(ctx,
		`SELECT `+oidcConfigColumns+` FROM oidc_config WHERE id = true`).
		Scan(&config.Enabled, &config.Issuer, &config.ClientID, &config.ClientSecretEnc,
			&config.ButtonLabel, &config.UpdatedAt, &config.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return OIDCConfig{}, nil
	}
	if err != nil {
		return OIDCConfig{}, fmt.Errorf("get oidc config: %w", err)
	}
	return config, nil
}

// SaveOIDCConfig upserts the single configuration row. A nil
// ClientSecretEnc keeps the secret already stored. The optional updatedBy
// overrides config.UpdatedBy when present.
func (s *Store) SaveOIDCConfig(ctx context.Context, config OIDCConfig, updatedBy ...string) (OIDCConfig, error) {
	actor := config.UpdatedBy
	if len(updatedBy) > 0 {
		actor = updatedBy[0]
	}

	var saved OIDCConfig
	err := s.pool.QueryRow(ctx,
		`INSERT INTO oidc_config
		     (id, enabled, issuer, client_id, client_secret_enc, button_label, updated_by)
		 VALUES (true, $1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
		     enabled = EXCLUDED.enabled,
		     issuer = EXCLUDED.issuer,
		     client_id = EXCLUDED.client_id,
		     client_secret_enc = COALESCE(EXCLUDED.client_secret_enc, oidc_config.client_secret_enc),
		     button_label = EXCLUDED.button_label,
		     updated_at = now(),
		     updated_by = EXCLUDED.updated_by
		 RETURNING `+oidcConfigColumns,
		config.Enabled, config.Issuer, config.ClientID, config.ClientSecretEnc,
		config.ButtonLabel, actor).
		Scan(&saved.Enabled, &saved.Issuer, &saved.ClientID, &saved.ClientSecretEnc,
			&saved.ButtonLabel, &saved.UpdatedAt, &saved.UpdatedBy)
	if err != nil {
		return OIDCConfig{}, fmt.Errorf("save oidc config: %w", err)
	}
	return saved, nil
}
