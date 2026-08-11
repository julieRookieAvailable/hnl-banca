package idempotency

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store guarda la respuesta de operaciones de dinero para poder responder de
// forma idéntica ante reintentos con la misma idempotency key.
type Store interface {
	Get(ctx context.Context, userID, key string) (json.RawMessage, bool, error)
	Set(ctx context.Context, userID, key string, value any) error
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Get(ctx context.Context, userID, key string) (json.RawMessage, bool, error) {
	var response json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT response FROM idempotency_keys WHERE user_id = $1 AND idempotency_key = $2`,
		userID, key).Scan(&response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return response, true, nil
}

func (s *PostgresStore) Set(ctx context.Context, userID, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO idempotency_keys (user_id, idempotency_key, response)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, idempotency_key) DO NOTHING`,
		userID, key, string(encoded))
	return err
}
