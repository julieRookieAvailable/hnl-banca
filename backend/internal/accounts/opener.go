package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
)

// Opener crea la cuenta bancaria de un usuario recién registrado: primero la
// cuenta en TigerBeetle (fuente de verdad del dinero) y después su metadato en
// Postgres (bank_accounts).
type Opener struct {
	repo   AccountRepository
	ledger tigerbeetle.LedgerClient
}

func NewOpener(repo AccountRepository, ledger tigerbeetle.LedgerClient) *Opener {
	return &Opener{repo: repo, ledger: ledger}
}

// Open genera un número de cuenta único derivado del usuario, crea la cuenta en
// TigerBeetle y persiste el metadato. Si el número colisiona (prácticamente
// imposible), se reintenta con otro derivado del mismo id.
func (o *Opener) Open(ctx context.Context, userID, accountType, currency string) (Account, error) {
	if currency == "" {
		currency = "USD"
	}
	for attempt := 0; attempt < 5; attempt++ {
		number := newAccountNumber(userID, attempt)
		id, err := tigerbeetle.AccountIDFromNumber(number)
		if err != nil {
			return Account{}, err
		}
		// Idempotente: si la cuenta ya existía, TB responde AccountExists y no falla.
		if err := o.ledger.CreateAccounts(ctx, []tigerbeetle.AccountSpec{
			{ID: id, Code: tigerbeetle.CodeForType(accountType)},
		}); err != nil {
			return Account{}, err
		}
		if err := o.repo.Create(ctx, Account{
			UserID:       userID,
			AccountNumber: number,
			AccountType:   accountType,
			Currency:      currency,
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				continue
			}
			return Account{}, err
		}
		return Account{
			UserID:       userID,
			AccountNumber: number,
			AccountType:   accountType,
			Currency:      currency,
		}, nil
	}
	return Account{}, errors.New("no se pudo generar un número de cuenta único")
}

// newAccountNumber genera un número con el mismo formato del seed
// (4001-XXXX-XXXX-<tb id>) con un id TigerBeetle derivado del id de usuario
// para no colisionar con las cuentas sembradas.
func newAccountNumber(userID string, salt int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", userID, salt)))
	tbID := binary.BigEndian.Uint64(h[:8]) >> 1 // cabe en int64 positivo
	if tbID < 1_000_000 {
		tbID += 1_000_000
	}
	return fmt.Sprintf("4001-%04X-%04X-%d",
		binary.BigEndian.Uint32(h[8:12])&0xFFFF,
		binary.BigEndian.Uint32(h[12:16])&0xFFFF,
		tbID)
}
