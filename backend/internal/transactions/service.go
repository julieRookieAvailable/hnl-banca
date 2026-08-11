package transactions

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
)

var (
	ErrInvalidInput     = errors.New("datos de transferencia inválidos")
	ErrInsufficientFunds = errors.New("saldo insuficiente")
	ErrDestNotFound     = errors.New("cuenta destino no existe")
	ErrDestForbidden    = errors.New("no tienes acceso a esta cuenta")
)

type Transaction struct {
	ID          int64
	FromAccount string
	ToAccount   string
	Type        string
	AmountCents int64
	Description string
	Timestamp   time.Time
	Status      string
}

// TransactionRepository es el contrato de persistencia de movimientos.
type TransactionRepository interface {
	Create(ctx context.Context, t Transaction) (int64, error)
	ListByAccount(ctx context.Context, accountNumber string, limit int) ([]Transaction, error)
	Exists(ctx context.Context, accountNumber string) (bool, error)
}

type PostgresTransactionRepository struct{ pool *pgxpool.Pool }

func NewPostgresTransactionRepository(pool *pgxpool.Pool) *PostgresTransactionRepository {
	return &PostgresTransactionRepository{pool: pool}
}

func (r *PostgresTransactionRepository) Create(ctx context.Context, t Transaction) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO transactions (from_account, to_account, type, amount, description, timestamp, status)
		 VALUES ($1, $2, $3, $4, $5, now(), 'completed')
		 RETURNING id`,
		t.FromAccount, t.ToAccount, t.Type, float64(t.AmountCents)/100, t.Description).Scan(&id)
	return id, err
}

func (r *PostgresTransactionRepository) ListByAccount(ctx context.Context, accountNumber string, limit int) ([]Transaction, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, from_account, to_account, type, amount, description, timestamp, status
		 FROM transactions
		 WHERE from_account = $1 OR to_account = $1
		 ORDER BY timestamp DESC, id DESC
		 LIMIT $2`,
		accountNumber, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]Transaction, 0)
	for rows.Next() {
		var t Transaction
		var amount float64
		if err := rows.Scan(&t.ID, &t.FromAccount, &t.ToAccount, &t.Type, &amount, &t.Description, &t.Timestamp, &t.Status); err != nil {
			return nil, err
		}
		t.AmountCents = int64(amount * 100)
		list = append(list, t)
	}
	return list, rows.Err()
}

func (r *PostgresTransactionRepository) Exists(ctx context.Context, accountNumber string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT true FROM bank_accounts WHERE account_number = $1`, accountNumber).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Service contiene la lógica de negocio de transferencias y es la única pieza
// que habla con TigerBeetle, de modo que se puede probar con mocks.
type Service struct {
	accounts accounts.AccountRepository
	txs      TransactionRepository
	ledger   tigerbeetle.LedgerClient
}

func NewService(accountsRepo accounts.AccountRepository, txs TransactionRepository, ledger tigerbeetle.LedgerClient) *Service {
	return &Service{accounts: accountsRepo, txs: txs, ledger: ledger}
}

type TransferInput struct {
	UserID         string
	FromAccount    string
	ToAccount      string
	AmountCents    int64
	Description    string
	IdempotencyKey string
}

func (s *Service) Transfer(ctx context.Context, in TransferInput) (*Transaction, error) {
	if in.FromAccount == "" || in.ToAccount == "" || in.AmountCents <= 0 {
		return nil, ErrInvalidInput
	}
	if in.FromAccount == in.ToAccount {
		return nil, ErrInvalidInput
	}

	ok, err := s.accounts.OwnedBy(ctx, in.UserID, in.FromAccount)
	if err != nil {
		if errors.Is(err, accounts.ErrNotFound) {
			return nil, ErrInvalidInput
		}
		return nil, err
	}
	if !ok {
		return nil, ErrDestForbidden
	}

	fromTB, err := tigerbeetle.AccountIDFromNumber(in.FromAccount)
	if err != nil {
		return nil, ErrInvalidInput
	}

	destID := s.ledger.ExternalID()
	txType := "withdrawal"
	if in.ToAccount != "EXTERNAL" {
		exists, err := s.txs.Exists(ctx, in.ToAccount)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrDestNotFound
		}
		destID, err = tigerbeetle.AccountIDFromNumber(in.ToAccount)
		if err != nil {
			return nil, ErrInvalidInput
		}
		txType = "transfer"
	}

	views, err := s.ledger.Balances(ctx, []tigerbeetle.AccountID{fromTB})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 || in.AmountCents > views[0].BalanceCents {
		return nil, ErrInsufficientFunds
	}

	txID := tigerbeetle.DeterministicTransferID(in.UserID, in.IdempotencyKey)
	if in.IdempotencyKey == "" {
		txID = tigerbeetle.NewID()
	}
	err = s.ledger.CreateTransfers(ctx, []tigerbeetle.TransferSpec{{
		ID:              txID,
		DebitAccountID:  fromTB,
		CreditAccountID: destID,
		AmountCents:     uint64(in.AmountCents),
	}})
	if err != nil {
		return nil, err
	}

	createdID, err := s.txs.Create(ctx, Transaction{
		FromAccount: in.FromAccount,
		ToAccount:   in.ToAccount,
		Type:        txType,
		AmountCents: in.AmountCents,
		Description: in.Description,
		Status:      "completed",
	})
	if err != nil {
		return nil, err
	}

	return &Transaction{
		ID:          createdID,
		FromAccount: in.FromAccount,
		ToAccount:   in.ToAccount,
		Type:        txType,
		AmountCents: in.AmountCents,
		Description: in.Description,
		Timestamp:   time.Now(),
		Status:      "completed",
	}, nil
}
