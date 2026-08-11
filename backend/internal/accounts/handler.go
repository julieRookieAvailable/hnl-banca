package accounts

import (
	"errors"
	"net/http"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/middleware"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
)

type AccountView struct {
	AccountNumber string `json:"account_number"`
	AccountType   string `json:"account_type"`
	Currency      string `json:"currency"`
	BalanceCents  int64  `json:"balance_cents"`
}

type Handler struct {
	cfg     *config.Config
	repo    AccountRepository
	ledger  tigerbeetle.LedgerClient
}

func NewHandler(cfg *config.Config, repo AccountRepository, ledger tigerbeetle.LedgerClient) *Handler {
	return &Handler{cfg: cfg, repo: repo, ledger: ledger}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())
	accs, err := h.repo.ListByUser(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "DB_ACCOUNTS_LIST", "error al consultar cuentas")
		return
	}

	ids := make([]tigerbeetle.AccountID, 0, len(accs))
	byID := make(map[tigerbeetle.AccountID]int)
	for i, a := range accs {
		id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		byID[id] = i
	}

	views, err := h.ledger.Balances(r.Context(), ids)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "TB_BALANCES", "error al consultar saldos")
		return
	}

	out := make([]AccountView, 0, len(accs))
	for _, v := range views {
		i, ok := byID[v.AccountID]
		if !ok {
			continue
		}
		out = append(out, AccountView{
			AccountNumber: accs[i].AccountNumber,
			AccountType:   accs[i].AccountType,
			Currency:      accs[i].Currency,
			BalanceCents:  v.BalanceCents,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	accountNumber := r.PathValue("accountNumber")
	userID := middleware.UserID(r.Context())
	ok, err := h.repo.OwnedBy(r.Context(), userID, accountNumber)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "cuenta no encontrada")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "DB_ACCOUNTS_GET", "error al consultar cuenta")
		return
	}
	if !ok {
		respond.Error(w, http.StatusForbidden, "ACCOUNT_FORBIDDEN", "no tienes acceso a esta cuenta")
		return
	}

	a, err := h.repo.ByNumber(r.Context(), accountNumber)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "cuenta no encontrada")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "DB_ACCOUNTS_GET", "error al consultar cuenta")
		return
	}

	id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "ACCOUNT_NUMBER_INVALID", "número de cuenta inválido")
		return
	}
	views, err := h.ledger.Balances(r.Context(), []tigerbeetle.AccountID{id})
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "TB_BALANCES", "error al consultar saldos")
		return
	}
	if len(views) == 0 {
		respond.Error(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "cuenta no encontrada")
		return
	}

	respond.JSON(w, http.StatusOK, AccountView{
		AccountNumber: a.AccountNumber,
		AccountType:   a.AccountType,
		Currency:      a.Currency,
		BalanceCents:  views[0].BalanceCents,
	})
}
