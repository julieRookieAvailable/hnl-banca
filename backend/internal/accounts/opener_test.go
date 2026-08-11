package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
	tb "github.com/tigerbeetle/tigerbeetle-go"
)

type openerFakeRepo struct {
	created []Account
	collide map[string]bool // números que "ya existen" en la base
}

func (f *openerFakeRepo) Create(ctx context.Context, a Account) error {
	if f.collide[a.AccountNumber] {
		return &pgconn.PgError{Code: "23505"}
	}
	f.created = append(f.created, a)
	return nil
}

func (f *openerFakeRepo) OwnedBy(ctx context.Context, userID, accountNumber string) (bool, error) {
	return false, ErrNotFound
}

func (f *openerFakeRepo) ByNumber(ctx context.Context, accountNumber string) (Account, error) {
	return Account{}, ErrNotFound
}

func (f *openerFakeRepo) ListByUser(ctx context.Context, userID string) ([]Account, error) {
	return nil, nil
}

type openerFakeLedger struct {
	accounts []tigerbeetle.AccountSpec
	err      error
}

func (f *openerFakeLedger) ExternalID() tb.Uint128 { return tb.ToUint128(9000001) }

func (f *openerFakeLedger) CreateAccounts(ctx context.Context, specs []tigerbeetle.AccountSpec) error {
	if f.err != nil {
		return f.err
	}
	f.accounts = append(f.accounts, specs...)
	return nil
}

func (f *openerFakeLedger) CreateTransfers(ctx context.Context, specs []tigerbeetle.TransferSpec) error {
	return f.err
}

func (f *openerFakeLedger) Balances(ctx context.Context, ids []tb.Uint128) ([]tigerbeetle.BalanceView, error) {
	return nil, nil
}

func (f *openerFakeLedger) CreatePending(ctx context.Context, debit, credit tb.Uint128, amountCents uint64) (tb.Uint128, error) {
	return tb.Uint128{}, nil
}

func (f *openerFakeLedger) PostPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	return nil
}

func (f *openerFakeLedger) VoidPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	return nil
}

func TestOpenCreatesAccount(t *testing.T) {
	repo := &openerFakeRepo{}
	ledger := &openerFakeLedger{}
	opener := NewOpener(repo, ledger)

	a, err := opener.Open(context.Background(), "u1", "checking", "USD")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.AccountNumber == "" {
		t.Fatal("número de cuenta vacío")
	}
	id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
	if err != nil {
		t.Fatalf("número de cuenta inválido: %v", err)
	}
	if len(ledger.accounts) != 1 {
		t.Fatalf("esperaba 1 cuenta TB, got %d", len(ledger.accounts))
	}
	if ledger.accounts[0].ID != id {
		t.Fatal("el id TB debe derivarse del último segmento del número")
	}
	if len(repo.created) != 1 {
		t.Fatalf("esperaba 1 cuenta PG, got %d", len(repo.created))
	}
	if repo.created[0].AccountType != "checking" || repo.created[0].Currency != "USD" {
		t.Fatalf("metadato incorrecto: %+v", repo.created[0])
	}
}

func TestOpenDefaultsCurrency(t *testing.T) {
	repo := &openerFakeRepo{}
	opener := NewOpener(repo, &openerFakeLedger{})

	if _, err := opener.Open(context.Background(), "u1", "savings", ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if repo.created[0].Currency != "USD" {
		t.Fatalf("currency por defecto esperado USD, got %s", repo.created[0].Currency)
	}
}

func TestOpenRetriesOnCollision(t *testing.T) {
	first := newAccountNumber("u1", 0)
	repo := &openerFakeRepo{collide: map[string]bool{first: true}}
	ledger := &openerFakeLedger{}
	opener := NewOpener(repo, ledger)

	a, err := opener.Open(context.Background(), "u1", "checking", "USD")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.AccountNumber == first {
		t.Fatal("no debió reutilizar el número colisionado")
	}
	if len(ledger.accounts) != 2 {
		t.Fatalf("esperaba 2 intentos en TB, got %d", len(ledger.accounts))
	}
	if len(repo.created) != 1 {
		t.Fatalf("esperaba 1 cuenta PG, got %d", len(repo.created))
	}
}

func TestOpenFailsWhenLedgerDown(t *testing.T) {
	opener := NewOpener(&openerFakeRepo{}, &openerFakeLedger{err: errors.New("tb no disponible")})
	if _, err := opener.Open(context.Background(), "u1", "checking", "USD"); err == nil {
		t.Fatal("esperaba error si TigerBeetle falla")
	}
}
