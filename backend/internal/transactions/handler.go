package transactions

import (
	"encoding/json"
	"errors"
	"net/http"
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

	txs, err := h.svc.txs.ListByAccount(r.Context(), accountNumber, 100)
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
