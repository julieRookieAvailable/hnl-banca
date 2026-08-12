package transactions

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/idempotency"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/middleware"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
)

type TransactionView struct {
	ID          int64  `json:"id"`
	FromAccount string `json:"from_account"`
	ToAccount   string `json:"to_account"`
	Type        string `json:"type"`
	AmountCents int64  `json:"amount_cents"`
	Description string `json:"description"`
	Timestamp   string `json:"timestamp"`
	Status      string `json:"status"`
}

type Handler struct {
	svc   *Service
	store idempotency.Store
}

func NewHandler(svc *Service, store idempotency.Store) *Handler {
	return &Handler{svc: svc, store: store}
}

func (h *Handler) ListByAccount(w http.ResponseWriter, r *http.Request) {
	accountNumber := r.PathValue("accountNumber")
	userID := middleware.UserID(r.Context())
	ok, err := h.svc.accounts.OwnedBy(r.Context(), userID, accountNumber)
	if err != nil {
		if errors.Is(err, accounts.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "cuenta no encontrada")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "DB_TXS_LIST", "error al consultar movimientos")
		return
	}
	if !ok {
		respond.Error(w, http.StatusForbidden, "ACCOUNT_FORBIDDEN", "no tienes acceso a esta cuenta")
		return
	}

	limit, offset := paginate(r, 100, 200)
	txs, err := h.svc.txs.ListByAccount(r.Context(), accountNumber, limit, offset)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "DB_TXS_LIST", "error al consultar movimientos")
		return
	}

	out := make([]TransactionView, 0, len(txs))
	for _, t := range txs {
		out = append(out, TransactionView{
			ID:          t.ID,
			FromAccount: t.FromAccount,
			ToAccount:   t.ToAccount,
			Type:        t.Type,
			AmountCents: t.AmountCents,
			Description: t.Description,
			Timestamp:   t.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Status:      t.Status,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

func (h *Handler) ListRecent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())
	txs, err := h.svc.txs.ListRecentByUser(r.Context(), userID, 5)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "DB_TXS_LIST", "error al consultar movimientos")
		return
	}
	out := make([]TransactionView, 0, len(txs))
	for _, t := range txs {
		out = append(out, TransactionView{
			ID:          t.ID,
			FromAccount: t.FromAccount,
			ToAccount:   t.ToAccount,
			Type:        t.Type,
			AmountCents: t.AmountCents,
			Description: t.Description,
			Timestamp:   t.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Status:      t.Status,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

type transferRequest struct {
	FromAccount string `json:"from_account"`
	ToAccount   string `json:"to_account"`
	AmountCents int64  `json:"amount_cents"`
	Description string `json:"description"`
}

func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())

	var req transferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return
	}
	req.FromAccount = strings.TrimSpace(req.FromAccount)
	req.ToAccount = strings.TrimSpace(req.ToAccount)

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		idemKey = uuid.NewString()
	}

	if cached, ok, err := h.store.Get(r.Context(), userID, idemKey); err == nil && ok {
		respond.JSONRaw(w, http.StatusOK, cached)
		return
	}

	created, err := h.svc.Transfer(r.Context(), TransferInput{
		UserID:         userID,
		FromAccount:    req.FromAccount,
		ToAccount:      req.ToAccount,
		AmountCents:    req.AmountCents,
		Description:    req.Description,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			respond.Error(w, http.StatusBadRequest, "INVALID_TRANSFER", "from_account, to_account y amount_cents son obligatorios y no pueden ser iguales")
		case errors.Is(err, ErrDestForbidden):
			respond.Error(w, http.StatusForbidden, "ACCOUNT_FORBIDDEN", "la cuenta de origen no es tuya")
		case errors.Is(err, ErrDestNotFound):
			respond.Error(w, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "cuenta destino no existe")
		case errors.Is(err, ErrInsufficientFunds):
			respond.Error(w, http.StatusBadRequest, "INSUFFICIENT_FUNDS", "saldo insuficiente")
		default:
			respond.Error(w, http.StatusInternalServerError, "TB_TRANSFER", "error al procesar la transferencia")
		}
		return
	}

	resp := TransactionView{
		ID:          created.ID,
		FromAccount: created.FromAccount,
		ToAccount:   created.ToAccount,
		Type:        created.Type,
		AmountCents: created.AmountCents,
		Description: created.Description,
		Timestamp:   created.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Status:      created.Status,
	}
	if r.Header.Get("Idempotency-Key") != "" {
		_ = h.store.Set(r.Context(), userID, idemKey, resp)
	}
	respond.JSON(w, http.StatusCreated, resp)
}

type depositRequest struct {
	AccountNumber string `json:"account_number"`
	AmountCents   int64  `json:"amount_cents"`
	Description   string `json:"description"`
}

func (h *Handler) Deposit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())

	var req depositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return
	}
	req.AccountNumber = strings.TrimSpace(req.AccountNumber)

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		idemKey = uuid.NewString()
	}

	if cached, ok, err := h.store.Get(r.Context(), userID, idemKey); err == nil && ok {
		respond.JSONRaw(w, http.StatusOK, cached)
		return
	}

	created, err := h.svc.Deposit(r.Context(), DepositInput{
		UserID:         userID,
		AccountNumber:  req.AccountNumber,
		AmountCents:    req.AmountCents,
		Description:    req.Description,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			respond.Error(w, http.StatusBadRequest, "INVALID_DEPOSIT", "account_number y amount_cents son obligatorios y amount_cents debe ser positivo")
		case errors.Is(err, ErrDestForbidden):
			respond.Error(w, http.StatusForbidden, "ACCOUNT_FORBIDDEN", "la cuenta no es tuya")
		default:
			respond.Error(w, http.StatusInternalServerError, "TB_DEPOSIT", "error al procesar el depósito")
		}
		return
	}

	resp := TransactionView{
		ID:          created.ID,
		FromAccount: created.FromAccount,
		ToAccount:   created.ToAccount,
		Type:        created.Type,
		AmountCents: created.AmountCents,
		Description: created.Description,
		Timestamp:   created.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Status:      created.Status,
	}
	if r.Header.Get("Idempotency-Key") != "" {
		_ = h.store.Set(r.Context(), userID, idemKey, resp)
	}
	respond.JSON(w, http.StatusCreated, resp)
}

type withdrawRequest struct {
	AccountNumber string `json:"account_number"`
	AmountCents   int64  `json:"amount_cents"`
	Description   string `json:"description"`
}

func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return
	}
	req.AccountNumber = strings.TrimSpace(req.AccountNumber)

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		idemKey = uuid.NewString()
	}

	if cached, ok, err := h.store.Get(r.Context(), userID, idemKey); err == nil && ok {
		respond.JSONRaw(w, http.StatusOK, cached)
		return
	}

	created, err := h.svc.Withdraw(r.Context(), WithdrawInput{
		UserID:         userID,
		AccountNumber:  req.AccountNumber,
		AmountCents:    req.AmountCents,
		Description:    req.Description,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			respond.Error(w, http.StatusBadRequest, "INVALID_WITHDRAWAL", "account_number y amount_cents son obligatorios y amount_cents debe ser positivo")
		case errors.Is(err, ErrDestForbidden):
			respond.Error(w, http.StatusForbidden, "ACCOUNT_FORBIDDEN", "la cuenta no es tuya")
		case errors.Is(err, ErrInsufficientFunds):
			respond.Error(w, http.StatusBadRequest, "INSUFFICIENT_FUNDS", "saldo insuficiente")
		default:
			respond.Error(w, http.StatusInternalServerError, "TB_WITHDRAWAL", "error al procesar el retiro")
		}
		return
	}

	resp := TransactionView{
		ID:          created.ID,
		FromAccount: created.FromAccount,
		ToAccount:   created.ToAccount,
		Type:        created.Type,
		AmountCents: created.AmountCents,
		Description: created.Description,
		Timestamp:   created.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Status:      created.Status,
	}
	if r.Header.Get("Idempotency-Key") != "" {
		_ = h.store.Set(r.Context(), userID, idemKey, resp)
	}
	respond.JSON(w, http.StatusCreated, resp)
}

// paginate interpreta los query params limit/offset con saneamiento: limit en
// [1, maxLimit] (default defLimit) y offset >= 0 (default 0).
func paginate(r *http.Request, defLimit, maxLimit int) (int, int) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = defLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}
	return limit, offset
}
