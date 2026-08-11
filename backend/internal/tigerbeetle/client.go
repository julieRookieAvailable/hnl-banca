// Package tigerbeetle envuelve el cliente Go de TigerBeetle con la convención
// del proyecto: un único ledger, códigos por tipo de cuenta y el id de cuenta
// derivado del último segmento del número de cuenta ("4001-...-0001" -> 1).
//
// LedgerClient es la interfaz que usan handlers y orquestadores; Client es la
// implementación concreta. Todos los métodos aceptan context.Context para
// poder aplicar timeouts de forma uniforme.
package tigerbeetle

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	tb "github.com/tigerbeetle/tigerbeetle-go"
)

const (
	Ledger = 1

	CodeChecking   uint16 = 1
	CodeSavings    uint16 = 2
	CodeInvestment uint16 = 3
	CodeExternal   uint16 = 9

	CodeTransfer uint16 = 10

	PendingTimeoutSeconds = uint32(300)

	batchSize = 1000
)

// Errores sentinela normalizados (independientes del status crudo de TB).
var (
	ErrExists         = errors.New("la transferencia ya existe")
	ErrAlreadyPosted  = errors.New("la transferencia pendiente ya fue aplicada")
	ErrAlreadyVoided  = errors.New("la transferencia pendiente ya fue cancelada")
	ErrPendingExpired = errors.New("la transferencia pendiente expiró")
)

type AccountSpec struct {
	ID   tb.Uint128
	Code uint16
}

type TransferSpec struct {
	ID              tb.Uint128
	DebitAccountID  tb.Uint128
	CreditAccountID tb.Uint128
	AmountCents     uint64
	PendingID       tb.Uint128
	Timeout         uint32
	Pending         bool
	PostPending     bool
	VoidPending     bool
}

type BalanceView struct {
	AccountID    tb.Uint128
	BalanceCents int64
}

type AccountID = tb.Uint128

// LedgerClient es el contrato que el resto de la aplicación consume.
type LedgerClient interface {
	ExternalID() tb.Uint128
	CreateAccounts(ctx context.Context, specs []AccountSpec) error
	CreateTransfers(ctx context.Context, specs []TransferSpec) error
	// Balances devuelve los saldos de varias cuentas en UNA sola petición batch.
	Balances(ctx context.Context, ids []tb.Uint128) ([]BalanceView, error)
	CreatePending(ctx context.Context, debit, credit tb.Uint128, amountCents uint64) (tb.Uint128, error)
	PostPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error
	VoidPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error
}

type Client struct {
	raw        tb.Client
	externalID tb.Uint128
	timeout    time.Duration
}

func NewClient(clusterID uint32, address string, externalAccountID uint64) (*Client, error) {
	raw, err := tb.NewClient(tb.ToUint128(uint64(clusterID)), []string{address})
	if err != nil {
		return nil, err
	}
	return &Client{raw: raw, externalID: tb.ToUint128(externalAccountID), timeout: 10 * time.Second}, nil
}

func (c *Client) Close() { c.raw.Close() }

func (c *Client) ExternalID() tb.Uint128 { return c.externalID }

// run ejecuta una operación de TB respetando la cancelación/timeout del contexto.
func (c *Client) run(ctx context.Context, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runV[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		v T
		e error
	}
	done := make(chan result, 1)
	go func() {
		v, e := fn()
		done <- result{v: v, e: e}
	}()
	select {
	case r := <-done:
		return r.v, r.e
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

func (c *Client) CreateAccounts(ctx context.Context, specs []AccountSpec) error {
	accounts := make([]tb.Account, 0, len(specs))
	for _, s := range specs {
		accounts = append(accounts, tb.Account{ID: s.ID, Ledger: Ledger, Code: s.Code})
	}
	for i := 0; i < len(accounts); i += batchSize {
		end := min(i+batchSize, len(accounts))
		chunk := accounts[i:end]
		results, err := runV(ctx, func() ([]tb.CreateAccountResult, error) {
			return c.raw.CreateAccounts(chunk)
		})
		if err != nil {
			return err
		}
		for _, r := range results {
			if r.Status != tb.AccountCreated && r.Status != tb.AccountExists {
				return fmt.Errorf("creando cuenta tb: %s", r.Status)
			}
		}
	}
	return nil
}

func (c *Client) CreateTransfers(ctx context.Context, specs []TransferSpec) error {
	transfers := make([]tb.Transfer, 0, len(specs))
	for _, s := range specs {
		transfers = append(transfers, buildTransfer(s))
	}
	for i := 0; i < len(transfers); i += batchSize {
		end := min(i+batchSize, len(transfers))
		chunk := transfers[i:end]
		results, err := runV(ctx, func() ([]tb.CreateTransferResult, error) {
			return c.raw.CreateTransfers(chunk)
		})
		if err != nil {
			return err
		}
		for _, r := range results {
			if err := mapTransferStatus(r.Status); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildTransfer(s TransferSpec) tb.Transfer {
	var flags uint16
	switch {
	case s.Pending:
		flags |= 1 << 1
	case s.PostPending:
		flags |= 1 << 2
	case s.VoidPending:
		flags |= 1 << 3
	}
	return tb.Transfer{
		ID:              s.ID,
		DebitAccountID:  s.DebitAccountID,
		CreditAccountID: s.CreditAccountID,
		Amount:          tb.ToUint128(s.AmountCents),
		PendingID:       s.PendingID,
		Timeout:         s.Timeout,
		Ledger:          Ledger,
		Code:            CodeTransfer,
		Flags:           flags,
	}
}

func mapTransferStatus(s tb.CreateTransferStatus) error {
	switch s {
	case tb.TransferCreated:
		return nil
	case tb.TransferExists:
		return ErrExists
	case tb.TransferPendingTransferAlreadyPosted:
		return ErrAlreadyPosted
	case tb.TransferPendingTransferAlreadyVoided:
		return ErrAlreadyVoided
	case tb.TransferPendingTransferExpired:
		return ErrPendingExpired
	default:
		return fmt.Errorf("creando transferencia tb: %s", s)
	}
}

// Balances consulta los saldos de muchas cuentas en una sola llamada batch.
func (c *Client) Balances(ctx context.Context, ids []tb.Uint128) ([]BalanceView, error) {
	if len(ids) == 0 {
		return []BalanceView{}, nil
	}
	accounts, err := runV(ctx, func() ([]tb.Account, error) {
		return c.raw.LookupAccounts(ids)
	})
	if err != nil {
		return nil, err
	}
	views := make([]BalanceView, 0, len(accounts))
	for _, a := range accounts {
		debits := new(big.Int).Add(a.DebitsPosted.BigInt(), a.DebitsPending.BigInt())
		credits := new(big.Int).Add(a.CreditsPosted.BigInt(), a.CreditsPending.BigInt())
		views = append(views, BalanceView{
			AccountID:    a.ID,
			BalanceCents: new(big.Int).Sub(credits, debits).Int64(),
		})
	}
	return views, nil
}

func (c *Client) Balance(ctx context.Context, id tb.Uint128) (int64, error) {
	views, err := c.Balances(ctx, []tb.Uint128{id})
	if err != nil {
		return 0, err
	}
	if len(views) == 0 {
		return 0, nil
	}
	return views[0].BalanceCents, nil
}

func (c *Client) CreatePending(ctx context.Context, debit, credit tb.Uint128, amountCents uint64) (tb.Uint128, error) {
	id := tb.ID()
	spec := TransferSpec{
		ID:              id,
		DebitAccountID:  debit,
		CreditAccountID: credit,
		AmountCents:     amountCents,
		Pending:         true,
		Timeout:         PendingTimeoutSeconds,
	}
	err := c.CreateTransfers(ctx, []TransferSpec{spec})
	if err != nil {
		return tb.Uint128{}, err
	}
	return id, nil
}

func (c *Client) PostPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	return c.CreateTransfers(ctx, []TransferSpec{{
		ID:              tb.ID(),
		DebitAccountID:  debit,
		CreditAccountID: credit,
		AmountCents:     amountCents,
		PendingID:       id,
		PostPending:     true,
	}})
}

func (c *Client) VoidPending(ctx context.Context, id, debit, credit tb.Uint128, amountCents uint64) error {
	return c.CreateTransfers(ctx, []TransferSpec{{
		ID:              tb.ID(),
		DebitAccountID:  debit,
		CreditAccountID: credit,
		AmountCents:     amountCents,
		PendingID:       id,
		VoidPending:     true,
	}})
}

func CodeForType(accountType string) uint16 {
	switch accountType {
	case "checking":
		return CodeChecking
	case "savings":
		return CodeSavings
	case "investment":
		return CodeInvestment
	default:
		return CodeExternal
	}
}

func TypeForCode(code uint16) string {
	switch code {
	case CodeChecking:
		return "checking"
	case CodeSavings:
		return "savings"
	case CodeInvestment:
		return "investment"
	default:
		return "external"
	}
}

// AccountIDFromNumber deriva el id TigerBeetle del último segmento del número de cuenta.
func AccountIDFromNumber(accountNumber string) (tb.Uint128, error) {
	parts := strings.Split(accountNumber, "-")
	if len(parts) == 0 {
		return tb.Uint128{}, errors.New("número de cuenta inválido")
	}
	id, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	if err != nil || id == 0 {
		return tb.Uint128{}, errors.New("número de cuenta inválido")
	}
	return tb.ToUint128(id), nil
}

// DeterministicTransferID genera un id TB determinista a partir de la idempotency
// key del usuario, de modo que reintentar una operación no duplique el movimiento.
func DeterministicTransferID(userID, idempotencyKey string) tb.Uint128 {
	h := sha256.Sum256([]byte(userID + ":" + idempotencyKey))
	var b [16]byte
	copy(b[:], h[:16])
	return tb.BytesToUint128(b)
}

// NewID devuelve un id TB aleatorio, útil cuando no hay idempotency key.
func NewID() tb.Uint128 { return tb.ID() }

// HexToID convierte una representación hexadecimal de 128 bits en un id TB.
func HexToID(hex string) (tb.Uint128, error) { return tb.HexStringToUint128(hex) }
