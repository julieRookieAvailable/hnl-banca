package chat

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrPendingNotFound   = errors.New("movimiento pendiente no encontrado")
	ErrPendingNotYours   = errors.New("el movimiento pendiente no te pertenece")
	ErrPendingProcessed  = errors.New("el movimiento pendiente ya fue procesado")
)

type PendingTransfer struct {
	UserID       string
	PendingID    string
	FromAccount  string
	ToAccount    string
	AmountCents  int64
	Description  string
	Status       string
	CreatedAt    time.Time
}

// PendingStore es el almacenamiento de transferencias pendientes (dos fases).
type PendingStore interface {
	Create(ctx context.Context, p PendingTransfer) error
	ByPendingID(ctx context.Context, pendingID string) (PendingTransfer, error)
	SetStatus(ctx context.Context, pendingID, status string) error
	SweepExpired(ctx context.Context, cutoff time.Time) (int64, error)
}

type PostgresPendingStore struct{ pool *pgxpool.Pool }

func NewPostgresPendingStore(pool *pgxpool.Pool) *PostgresPendingStore {
	return &PostgresPendingStore{pool: pool}
}

func (s *PostgresPendingStore) Create(ctx context.Context, p PendingTransfer) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO pending_transfers (user_id, tb_pending_id, from_account, to_account, amount_cents, description, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')`,
		p.UserID, p.PendingID, p.FromAccount, p.ToAccount, p.AmountCents, p.Description)
	return err
}

func (s *PostgresPendingStore) ByPendingID(ctx context.Context, pendingID string) (PendingTransfer, error) {
	var p PendingTransfer
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, tb_pending_id, from_account, to_account, amount_cents, description, status
		 FROM pending_transfers WHERE tb_pending_id = $1`,
		pendingID).Scan(&p.UserID, &p.PendingID, &p.FromAccount, &p.ToAccount, &p.AmountCents, &p.Description, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingTransfer{}, ErrPendingNotFound
	}
	if err != nil {
		return PendingTransfer{}, err
	}
	return p, nil
}

func (s *PostgresPendingStore) SetStatus(ctx context.Context, pendingID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE pending_transfers SET status = $2 WHERE tb_pending_id = $1`, pendingID, status)
	return err
}

// SweepExpired marca como voided las transferencias pendientes que superaron el
// cutoff. TigerBeetle ya las revirtió automáticamente al vencer su timeout, por
// lo que aquí solo se refleja ese estado real en Postgres.
func (s *PostgresPendingStore) SweepExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE pending_transfers SET status = 'voided'
		 WHERE status = 'pending' AND created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
