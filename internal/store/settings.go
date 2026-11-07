package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type PlatformSettings struct {
	ActivationFeeAmount  int64     `json:"activationFeeAmount"`
	ProcessingFeePercent int       `json:"processingFeePercent"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

func readSettings(ctx context.Context, q rowQuerier) (PlatformSettings, error) {
	var s PlatformSettings
	err := q.QueryRow(ctx,
		`SELECT activation_fee_amount, processing_fee_percent, updated_at FROM platform_settings`).
		Scan(&s.ActivationFeeAmount, &s.ProcessingFeePercent, &s.UpdatedAt)
	return s, err
}

func (p *Postgres) Settings(ctx context.Context) (PlatformSettings, error) {
	return readSettings(ctx, p.pool)
}

func (p *Postgres) UpdateSettings(ctx context.Context, actorID string, in PlatformSettings) (PlatformSettings, error) {
	var s PlatformSettings
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`UPDATE platform_settings SET activation_fee_amount = $1, processing_fee_percent = $2,
			 updated_at = now(), updated_by = $3
			 RETURNING activation_fee_amount, processing_fee_percent, updated_at`,
			in.ActivationFeeAmount, in.ProcessingFeePercent, actorID).
			Scan(&s.ActivationFeeAmount, &s.ProcessingFeePercent, &s.UpdatedAt); err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{actorID: actorID, action: "settings.updated", entityType: "platform_settings",
			details: map[string]any{"activationFeeAmount": s.ActivationFeeAmount,
				"processingFeePercent": s.ProcessingFeePercent}})
	})
	return s, err
}
