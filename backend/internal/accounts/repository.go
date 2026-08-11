package accounts

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
)

var (
	ErrNotFound   = errors.New("cuenta no encontrada")
	ErrForbidden  = errors.New("no tienes acceso a esta cuenta")
	ErrNoAccounts = errors.New("no hay cuentas asociadas al usuario")
)

type Account struct {
	ID           int64
	UserID       string
	AccountNumber string
	AccountType   string
	Currency      string
}

// AccountRepository es el contrato que consume el handler de cuentas.
type AccountRepository interface {
	Create(ctx context.Context, a Account) error
	OwnedBy(ctx context.Context, userID, accountNumber string) (bool, error)
	ByNumber(ctx context.Context, accountNumber string) (Account, error)
	ListByUser(ctx context.Context, userID string) ([]Account, error)
}

type PostgresAccountRepository struct{ pool *pgxpool.Pool }

func NewPostgresAccountRepository(pool *pgxpool.Pool) *PostgresAccountRepository {
	return &PostgresAccountRepository{pool: pool}
}

// Create inserta el metadato de una cuenta. El id TigerBeetle se deriva del
// número de cuenta (último segmento), igual que en el seed.
func (r *PostgresAccountRepository) Create(ctx context.Context, a Account) error {
	id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
	if err != nil {
		return err
	}
	lo, hi := id.Uint64()
	if hi != 0 {
		return errors.New("id tb fuera de rango int64")
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO bank_accounts (account_number, user_id, tb_account_id, account_type, currency)
		 VALUES ($1, $2, $3, $4, $5)`,
		a.AccountNumber, a.UserID, int64(lo), a.AccountType, a.Currency)
	return err
}

func (r *PostgresAccountRepository) OwnedBy(ctx context.Context, userID, accountNumber string) (bool, error) {
	var owner string
	err := r.pool.QueryRow(ctx,
		`SELECT user_id FROM bank_accounts WHERE account_number = $1`, accountNumber).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return owner == userID, nil
}

func (r *PostgresAccountRepository) ByNumber(ctx context.Context, accountNumber string) (Account, error) {
	var a Account
	err := r.pool.QueryRow(ctx,
		`SELECT account_number, account_type, currency FROM bank_accounts WHERE account_number = $1`,
		accountNumber).Scan(&a.AccountNumber, &a.AccountType, &a.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

func (r *PostgresAccountRepository) ListByUser(ctx context.Context, userID string) ([]Account, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT account_number, account_type, currency FROM bank_accounts WHERE user_id = $1 ORDER BY account_number`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]Account, 0)
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.AccountNumber, &a.AccountType, &a.Currency); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
