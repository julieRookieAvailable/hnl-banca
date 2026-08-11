package transactions

import (
	"context"
	"errors"
	"testing"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
	tb "github.com/tigerbeetle/tigerbeetle-go"
)

type fakeAccountRepo struct {
	owned map[string]string // accountNumber -> userID
}

func (f *fakeAccountRepo) OwnedBy(ctx context.Context, userID, accountNumber string) (bool, error) {
	owner, ok := f.owned[accountNumber]
	if !ok {
		return false, accounts.ErrNotFound
	}
	return owner == userID, nil
}

func (f *fakeAccountRepo) ByNumber(ctx context.Context, accountNumber string) (accounts.Account, error) {
	return accounts.Account{}, nil
}

func (f *fakeAccountRepo) ListByUser(ctx context.Context, userID string) ([]accounts.Account, error) {
	return nil, nil
}

type fakeTxRepo struct {
	created []Transaction
	nextID  int64
}

func (f *fakeTxRepo) Create(ctx context.Context, t Transaction) (int64, error) {
	f.nextID++
	f.created = append(f.created, t)
	return f.nextID, nil
}

func (f *fakeTxRepo) ListByAccount(ctx context.Context, accountNumber string, limit int) ([]Transaction, error) {
	return nil, nil
}

func (f *fakeTxRepo) Exists(ctx context.Context, accountNumber string) (bool, error) {
	return accountNumber != "EXTERNAL" && accountNumber != "9999-MISSING", nil
}

type fakeLedger struct {
	balances  map[uint64]int64
	transfers []tigerbeetle.TransferSpec
}

func (f *fakeLedger) ExternalID() tb.Uint128 { return tb.ToUint128(9000001) }

func (f *fakeLedger) CreateAccounts(ctx context.Context, specs []tigerbeetle.AccountSpec) error {
	return nil
}

func (f *fakeLedger) CreateTransfers(ctx context.Context, specs []tigerbeetle.TransferSpec) error {
	f.transfers = append(f.transfers, specs...)
	return nil
}

func (f *fakeLedger) Balances(ctx context.Context, ids []tb.Uint128) ([]tigerbeetle.BalanceView, error) {
	views := make([]tigerbeetle.BalanceView, 0, len(ids))
	for _, id := range ids {
		lo, _ := id.Uint64()
		views = append(views, tigerbeetle.BalanceView{AccountID: id, BalanceCents: f.balances[lo]})
	}
	return views, nil
}

func (f *fakeLedger) CreatePending(ctx context.Context, debit, credit tb.Uint128, amountCents uint64) (tb.Uint128, error) {
	return tb.ToUint128(1), nil
}

func (f *fakeLedger) PostPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	return nil
}

func (f *fakeLedger) VoidPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	return nil
}

func newTestService(owned map[string]string, balances map[uint64]int64) (*Service, *fakeTxRepo, *fakeLedger) {
	txRepo := &fakeTxRepo{}
	ledger := &fakeLedger{balances: balances}
	svc := NewService(&fakeAccountRepo{owned: owned}, txRepo, ledger)
	return svc, txRepo, ledger
}

func TestTransferSuccess(t *testing.T) {
	svc, txRepo, ledger := newTestService(
		map[string]string{"4001-0001-0001": "u1"},
		map[uint64]int64{1: 100000},
	)
	created, err := svc.Transfer(context.Background(), TransferInput{
		UserID:      "u1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "4001-0001-0002",
		AmountCents: 2500,
	})
	if err != nil {
		t.Fatalf("transfer esperada sin error, got %v", err)
	}
	if created.AmountCents != 2500 || created.Type != "transfer" {
		t.Fatalf("transferencia inesperada: %+v", created)
	}
	if len(ledger.transfers) != 1 {
		t.Fatalf("esperaba 1 transferencia en TB, got %d", len(ledger.transfers))
	}
	got := ledger.transfers[0]
	if got.AmountCents != 2500 {
		t.Fatalf("monto en TB incorrecto: %d", got.AmountCents)
	}
	if len(txRepo.created) != 1 {
		t.Fatalf("esperaba 1 registro en postgres, got %d", len(txRepo.created))
	}
}

func TestTransferInsufficientFunds(t *testing.T) {
	svc, _, _ := newTestService(
		map[string]string{"4001-0001-0001": "u1"},
		map[uint64]int64{1: 1000},
	)
	_, err := svc.Transfer(context.Background(), TransferInput{
		UserID:      "u1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "4001-0001-0002",
		AmountCents: 2500,
	})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("esperaba ErrInsufficientFunds, got %v", err)
	}
}

func TestTransferNotOwner(t *testing.T) {
	svc, _, _ := newTestService(
		map[string]string{"4001-0001-0001": "u1"},
		map[uint64]int64{1: 100000},
	)
	_, err := svc.Transfer(context.Background(), TransferInput{
		UserID:      "u2",
		FromAccount: "4001-0001-0001",
		ToAccount:   "4001-0001-0002",
		AmountCents: 2500,
	})
	if !errors.Is(err, ErrDestForbidden) {
		t.Fatalf("esperaba ErrDestForbidden, got %v", err)
	}
}

func TestTransferDestinationNotFound(t *testing.T) {
	svc, _, _ := newTestService(
		map[string]string{"4001-0001-0001": "u1"},
		map[uint64]int64{1: 100000},
	)
	_, err := svc.Transfer(context.Background(), TransferInput{
		UserID:      "u1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "9999-MISSING",
		AmountCents: 2500,
	})
	if !errors.Is(err, ErrDestNotFound) {
		t.Fatalf("esperaba ErrDestNotFound, got %v", err)
	}
}

func TestTransferWithdrawalToExternal(t *testing.T) {
	svc, txRepo, _ := newTestService(
		map[string]string{"4001-0001-0001": "u1"},
		map[uint64]int64{1: 100000},
	)
	created, err := svc.Transfer(context.Background(), TransferInput{
		UserID:      "u1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "EXTERNAL",
		AmountCents: 500,
	})
	if err != nil {
		t.Fatalf("retiro esperado sin error, got %v", err)
	}
	if created.Type != "withdrawal" {
		t.Fatalf("tipo esperado withdrawal, got %s", created.Type)
	}
	if txRepo.created[0].Type != "withdrawal" {
		t.Fatalf("registro en postgres con tipo incorrecto")
	}
}

func TestTransferInvalidInput(t *testing.T) {
	svc, _, _ := newTestService(nil, nil)
	_, err := svc.Transfer(context.Background(), TransferInput{
		UserID:      "u1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "4001-0001-0001",
		AmountCents: 100,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("esperaba ErrInvalidInput, got %v", err)
	}
}
