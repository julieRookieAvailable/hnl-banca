package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/transactions"
	tb "github.com/tigerbeetle/tigerbeetle-go"
)

type chatFakeAccounts struct {
	owned map[string]string
	list  []accounts.Account
}

func (f *chatFakeAccounts) OwnedBy(ctx context.Context, userID, accountNumber string) (bool, error) {
	owner, ok := f.owned[accountNumber]
	if !ok {
		return false, accounts.ErrNotFound
	}
	return owner == userID, nil
}

func (f *chatFakeAccounts) Create(ctx context.Context, a accounts.Account) error {
	return nil
}

func (f *chatFakeAccounts) ByNumber(ctx context.Context, accountNumber string) (accounts.Account, error) {
	return accounts.Account{}, nil
}

func (f *chatFakeAccounts) ListByUser(ctx context.Context, userID string) ([]accounts.Account, error) {
	return f.list, nil
}

type chatFakeTx struct {
	exists map[string]bool
	created []transactions.Transaction
}

func (f *chatFakeTx) Create(ctx context.Context, t transactions.Transaction) (int64, error) {
	f.created = append(f.created, t)
	return int64(len(f.created)), nil
}

func (f *chatFakeTx) ListByAccount(ctx context.Context, accountNumber string, limit, offset int) ([]transactions.Transaction, error) {
	return nil, nil
}

func (f *chatFakeTx) ListByAccountAll(ctx context.Context, accountNumber string) ([]transactions.Transaction, error) {
	return nil, nil
}

func (f *chatFakeTx) ListRecentByUser(ctx context.Context, userID string, limit int) ([]transactions.Transaction, error) {
	return nil, nil
}

func (f *chatFakeTx) Exists(ctx context.Context, accountNumber string) (bool, error) {
	ok, exists := f.exists[accountNumber]
	return ok && exists, nil
}

type chatFakeLedger struct {
	balances    map[uint64]int64
	postCalled  []tb.Uint128
	voidCalled  []tb.Uint128
}

func (f *chatFakeLedger) ExternalID() tb.Uint128 { return tb.ToUint128(9000001) }

func (f *chatFakeLedger) CreateAccounts(ctx context.Context, specs []tigerbeetle.AccountSpec) error {
	return nil
}

func (f *chatFakeLedger) CreateTransfers(ctx context.Context, specs []tigerbeetle.TransferSpec) error {
	return nil
}

func (f *chatFakeLedger) Balances(ctx context.Context, ids []tb.Uint128) ([]tigerbeetle.BalanceView, error) {
	views := make([]tigerbeetle.BalanceView, 0, len(ids))
	for _, id := range ids {
		lo, _ := id.Uint64()
		views = append(views, tigerbeetle.BalanceView{AccountID: id, BalanceCents: f.balances[lo]})
	}
	return views, nil
}

func (f *chatFakeLedger) CreatePending(ctx context.Context, debit, credit tb.Uint128, amountCents uint64) (tb.Uint128, error) {
	return tb.ToUint128(777), nil
}

func (f *chatFakeLedger) PostPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	f.postCalled = append(f.postCalled, id)
	return nil
}

func (f *chatFakeLedger) VoidPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	f.voidCalled = append(f.voidCalled, id)
	return nil
}

type fakeProvider struct {
	response *Result
	err      error
}

func (f *fakeProvider) Complete(ctx context.Context, messages []Message, tools []Tool) (*Result, error) {
	return f.response, f.err
}

type fakePendingStore struct {
	pending PendingTransfer
	err     error
}

func (f *fakePendingStore) Create(ctx context.Context, p PendingTransfer) error {
	if f.err != nil {
		return f.err
	}
	f.pending = p
	return nil
}

func (f *fakePendingStore) ByPendingID(ctx context.Context, pendingID string) (PendingTransfer, error) {
	if f.err != nil {
		return PendingTransfer{}, f.err
	}
	return f.pending, nil
}

func (f *fakePendingStore) SetStatus(ctx context.Context, pendingID, status string) error {
	if f.err != nil {
		return f.err
	}
	f.pending.Status = status
	return nil
}

func (f *fakePendingStore) SweepExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.pending.Status = "voided"
	return 1, nil
}

func newChatService() (*Service, *chatFakeLedger, *chatFakeTx, *fakePendingStore) {
	ledger := &chatFakeLedger{balances: map[uint64]int64{1: 100000}}
	accs := &chatFakeAccounts{
		owned: map[string]string{"4001-0001-0001": "u1"},
		list:  []accounts.Account{{AccountNumber: "4001-0001-0001", AccountType: "checking"}},
	}
	tx := &chatFakeTx{exists: map[string]bool{"4001-0001-0002": true}}
	pending := &fakePendingStore{}
	svc := NewService(accs, ledger, tx, pending, &fakeProvider{}, nil)
	return svc, ledger, tx, pending
}

func TestToolBalances(t *testing.T) {
	svc, _, _, _ := newChatService()
	out, _, err := svc.toolBalances(context.Background(), "u1")
	if err != nil {
		t.Fatalf("toolBalances: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("salida no es JSON válido: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("esperaba 1 cuenta, got %d", len(parsed))
	}
	if parsed[0]["balance_cents"].(float64) != 100000 {
		t.Fatalf("balance incorrecto: %v", parsed[0]["balance_cents"])
	}
}

func TestToolCreatePending(t *testing.T) {
	svc, _, _, pending := newChatService()
	args := json.RawMessage(`{"from_account":"4001-0001-0001","to_account":"4001-0001-0002","amount":150.25}`)
	_, action, err := svc.toolCreatePending(context.Background(), "u1", args)
	if err != nil {
		t.Fatalf("toolCreatePending: %v", err)
	}
	if action == nil || action.PendingID != "309" {
		t.Fatalf("acción inesperada: %+v", action)
	}
	if pending.pending.FromAccount != "4001-0001-0001" || pending.pending.AmountCents != 15025 {
		t.Fatalf("pending almacenado incorrecto: %+v", pending.pending)
	}
}

func TestToolCreatePendingInsufficientFunds(t *testing.T) {
	svc, _, _, _ := newChatService()
	args := json.RawMessage(`{"from_account":"4001-0001-0001","to_account":"EXTERNAL","amount":99999}`)
	_, _, err := svc.toolCreatePending(context.Background(), "u1", args)
	if err == nil {
		t.Fatal("esperaba error de saldo insuficiente")
	}
}

func TestToolCreatePendingDoubleEncodedArgs(t *testing.T) {
	svc, _, _, pending := newChatService()
	encoded := json.RawMessage(`"{\"from_account\":\"4001-0001-0001\",\"to_account\":\"4001-0001-0002\",\"amount\":150.25}"`)
	_, action, err := svc.toolCreatePending(context.Background(), "u1", encoded)
	if err != nil {
		t.Fatalf("toolCreatePending con args doble-codificados: %v", err)
	}
	if action == nil || action.AmountCents != 15025 {
		t.Fatalf("acción inesperada con args doble-codificados: %+v", action)
	}
	if pending.pending.AmountCents != 15025 {
		t.Fatalf("pending almacenado incorrecto: %+v", pending.pending)
	}
}

func TestExecuteToolViaMCP(t *testing.T) {
	svc, _, _, pending := newChatService()
	sess, err := svc.newMCPSession(context.Background(), "u1")
	if err != nil {
		t.Fatalf("newMCPSession: %v", err)
	}
	defer sess.close()

	content, action, err := svc.executeTool(context.Background(), sess, "get_balances", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("executeTool get_balances: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("salida de get_balances no es JSON: %v", err)
	}
	if len(parsed) != 1 || parsed[0]["balance_cents"].(float64) != 100000 {
		t.Fatalf("saldo inesperado: %+v", parsed)
	}
	if action != nil {
		t.Fatalf("get_balances no debería devolver acción: %+v", action)
	}

	_, action, err = svc.executeTool(context.Background(), sess, "create_pending_transfer",
		json.RawMessage(`{"from_account":"4001-0001-0001","to_account":"4001-0001-0002","amount":150.25}`))
	if err != nil {
		t.Fatalf("executeTool create_pending_transfer: %v", err)
	}
	if action == nil || action.PendingID != "309" || action.AmountCents != 15025 {
		t.Fatalf("acción inesperada a través de MCP: %+v", action)
	}
	if pending.pending.AmountCents != 15025 {
		t.Fatalf("pending almacenado incorrecto: %+v", pending.pending)
	}
}

func TestExecuteToolViaMCPDoubleEncodedArgs(t *testing.T) {
	svc, _, _, pending := newChatService()
	sess, err := svc.newMCPSession(context.Background(), "u1")
	if err != nil {
		t.Fatalf("newMCPSession: %v", err)
	}
	defer sess.close()

	encoded := json.RawMessage(`"{\"from_account\":\"4001-0001-0001\",\"to_account\":\"4001-0001-0002\",\"amount\":150.25}"`)
	_, action, err := svc.executeTool(context.Background(), sess, "create_pending_transfer", encoded)
	if err != nil {
		t.Fatalf("executeTool con args doble-codificados: %v", err)
	}
	if action == nil || action.AmountCents != 15025 {
		t.Fatalf("acción inesperada con args doble-codificados: %+v", action)
	}
	if pending.pending.AmountCents != 15025 {
		t.Fatalf("pending almacenado incorrecto: %+v", pending.pending)
	}
}

func TestExecuteToolViaMCPUnknown(t *testing.T) {
	svc, _, _, _ := newChatService()
	sess, err := svc.newMCPSession(context.Background(), "u1")
	if err != nil {
		t.Fatalf("newMCPSession: %v", err)
	}
	defer sess.close()

	_, _, err = svc.executeTool(context.Background(), sess, "no_such_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("esperaba error para herramienta desconocida")
	}
}

func TestResolvePendingConfirm(t *testing.T) {
	svc, ledger, tx, pending := newChatService()
	pending.pending = PendingTransfer{
		UserID:      "u1",
		PendingID:   "4d1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "4001-0001-0002",
		AmountCents: 5000,
		Status:      "pending",
	}
	if err := svc.resolvePending(context.Background(), "u1", "4d1", "confirm"); err != nil {
		t.Fatalf("resolvePending confirm: %v", err)
	}
	if len(ledger.postCalled) != 1 {
		t.Fatalf("esperaba 1 post en TB, got %d", len(ledger.postCalled))
	}
	if len(tx.created) != 1 || tx.created[0].Type != "transfer" {
		t.Fatalf("registro de transacción incorrecto: %+v", tx.created)
	}
	if pending.pending.Status != "completed" {
		t.Fatalf("status esperado completed, got %s", pending.pending.Status)
	}
}

func TestResolvePendingCancel(t *testing.T) {
	svc, ledger, _, pending := newChatService()
	pending.pending = PendingTransfer{
		UserID:      "u1",
		PendingID:   "4d1",
		FromAccount: "4001-0001-0001",
		ToAccount:   "EXTERNAL",
		AmountCents: 5000,
		Status:      "pending",
	}
	if err := svc.resolvePending(context.Background(), "u1", "4d1", "cancel"); err != nil {
		t.Fatalf("resolvePending cancel: %v", err)
	}
	if len(ledger.voidCalled) != 1 {
		t.Fatalf("esperaba 1 void en TB, got %d", len(ledger.voidCalled))
	}
	if pending.pending.Status != "voided" {
		t.Fatalf("status esperado voided, got %s", pending.pending.Status)
	}
}

func TestResolvePendingNotOwner(t *testing.T) {
	svc, _, _, pending := newChatService()
	pending.pending = PendingTransfer{UserID: "u1", PendingID: "4d1", Status: "pending"}
	err := svc.resolvePending(context.Background(), "u2", "4d1", "confirm")
	if !errors.Is(err, ErrPendingNotYours) {
		t.Fatalf("esperaba ErrPendingNotYours, got %v", err)
	}
}
