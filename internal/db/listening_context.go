package db

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultListeningContextScope = "global"

type ListeningContext struct {
	Scope     string
	Mode      string
	Mood      string
	ExpiresAt *time.Time
	UpdatedAt time.Time
	Source    string
}

func (c *Client) GetListeningContext(ctx context.Context) (*ListeningContext, error) {
	row := c.pool.QueryRow(
		ctx,
		`SELECT scope, mode, mood, expires_at, updated_at, source
		 FROM listening_context
		 WHERE scope = $1`,
		defaultListeningContextScope,
	)

	var item ListeningContext
	var expiresAt *time.Time
	if err := row.Scan(
		&item.Scope,
		&item.Mode,
		&item.Mood,
		&expiresAt,
		&item.UpdatedAt,
		&item.Source,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.Mode = strings.TrimSpace(item.Mode)
	item.Mood = strings.TrimSpace(item.Mood)
	item.Source = strings.TrimSpace(item.Source)
	item.ExpiresAt = expiresAt

	if item.ExpiresAt != nil && item.ExpiresAt.Before(time.Now().UTC()) {
		return nil, nil
	}
	return &item, nil
}

func (c *Client) UpsertListeningContext(ctx context.Context, mode, mood string, expiresAt *time.Time, source string) (*ListeningContext, error) {
	mode = strings.TrimSpace(mode)
	mood = strings.TrimSpace(mood)
	source = strings.TrimSpace(source)
	now := time.Now().UTC()

	if _, err := c.pool.Exec(
		ctx,
		`INSERT INTO listening_context (scope, mode, mood, expires_at, updated_at, source)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (scope) DO UPDATE SET
			mode = EXCLUDED.mode,
			mood = EXCLUDED.mood,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at,
			source = EXCLUDED.source`,
		defaultListeningContextScope,
		mode,
		mood,
		expiresAt,
		now,
		source,
	); err != nil {
		return nil, err
	}

	return &ListeningContext{
		Scope:     defaultListeningContextScope,
		Mode:      mode,
		Mood:      mood,
		ExpiresAt: expiresAt,
		UpdatedAt: now,
		Source:    source,
	}, nil
}

func (c *Client) DeleteListeningContext(ctx context.Context) error {
	_, err := c.pool.Exec(
		ctx,
		`DELETE FROM listening_context WHERE scope = $1`,
		defaultListeningContextScope,
	)
	return err
}
