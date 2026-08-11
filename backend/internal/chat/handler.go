package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/middleware"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/transactions"
)

const externalAccount = "EXTERNAL"

const systemPrompt = `Eres el asistente virtual de "Banca en Línea HNL". Ayudas a los clientes a consultar
sus saldos y realizar transferencias. Reglas obligatorias:
1. Para cualquier transferencia debes usar la herramienta create_pending_transfer, que crea un
   movimiento pendiente. Nunca confirmes ni canceles una transferencia sin la confirmación
   explícita del usuario en el mismo mensaje siguiente.
2. Después de crear un movimiento pendiente, informa al usuario el monto, origen y destino y
   pregúntale si lo confirma o lo cancela.
3. Si el usuario confirma, usa confirm_pending_transfer con el pending_id indicado. Si cancela,
   usa cancel_pending_transfer.
4. Sé breve, amable y en español. Usa montos en formato de moneda con 2 decimales.`

var toolDefinitions = []Tool{
	{
		Name:        "get_balances",
		Description: "Devuelve las cuentas del cliente autenticado con su saldo disponible en centavos.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
	{
		Name:        "create_pending_transfer",
		Description: "Crea una transferencia en estado pendiente (dos fases) que requiere confirmación explícita del usuario. to_account puede ser EXTERNAL.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"from_account":{"type":"string","description":"Número de cuenta de origen del cliente"},
				"to_account":{"type":"string","description":"Cuenta destino o EXTERNAL"},
				"amount":{"type":"number","description":"Monto en la moneda principal (ej. 150.25)"},
				"description":{"type":"string","description":"Motivo o descripción opcional"}
			},
			"required":["from_account","to_account","amount"],
			"additionalProperties":false
		}`),
	},
	{
		Name:        "confirm_pending_transfer",
		Description: "Confirma y aplica una transferencia pendiente usando su pending_id.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"pending_id":{"type":"string"}},"required":["pending_id"],"additionalProperties":false}`),
	},
	{
		Name:        "cancel_pending_transfer",
		Description: "Cancela (void) una transferencia pendiente usando su pending_id.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"pending_id":{"type":"string"}},"required":["pending_id"],"additionalProperties":false}`),
	},
}

type Action struct {
	Type        string `json:"type"`
	PendingID   string `json:"pending_id"`
	FromAccount string `json:"from_account"`
	ToAccount   string `json:"to_account"`
	AmountCents int64  `json:"amount_cents"`
	Description string `json:"description"`
}

type Service struct {
	accounts accounts.AccountRepository
	ledger   tigerbeetle.LedgerClient
	txs      transactions.TransactionRepository
	pendings PendingStore
	provider ChatProvider
}

func NewService(
	accountsRepo accounts.AccountRepository,
	ledger tigerbeetle.LedgerClient,
	txs transactions.TransactionRepository,
	pendings PendingStore,
	provider ChatProvider,
) *Service {
	return &Service{
		accounts: accountsRepo,
		ledger:   ledger,
		txs:      txs,
		pendings: pendings,
		provider: provider,
	}
}

type historyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Message string           `json:"message"`
	History []historyMessage `json:"history"`
}

type pendingRequest struct {
	PendingID string `json:"pending_id"`
}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		respond.Error(w, http.StatusBadRequest, "INVALID_MESSAGE", "message es obligatorio")
		return
	}
	if h.service.provider == nil {
		respond.Error(w, http.StatusServiceUnavailable, "CHAT_UNAVAILABLE", "el chat no está disponible en este momento")
		return
	}

	messages := []Message{{Role: "system", Content: systemPrompt}}
	for _, m := range req.History {
		if m.Role == "system" {
			continue
		}
		messages = append(messages, Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, Message{Role: "user", Content: req.Message})

	var finalReply string
	var action *Action
	for i := 0; i < 6; i++ {
		res, err := h.service.provider.Complete(r.Context(), messages, toolDefinitions)
		if err != nil {
			respond.Error(w, http.StatusBadGateway, "CHAT_PROVIDER_ERROR", "error del proveedor de IA: "+err.Error())
			return
		}
		if len(res.ToolCalls) == 0 {
			finalReply = res.Content
			break
		}

		messages = append(messages, Message{Role: "assistant", Content: res.Content, ToolCalls: res.ToolCalls})

		for _, tc := range res.ToolCalls {
			content, act, err := h.service.executeTool(r.Context(), userID, tc.Name, tc.Args)
			if err != nil {
				content = fmt.Sprintf("Error al ejecutar %s: %v", tc.Name, err)
			}
			if act != nil {
				action = act
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: tc.ID, Content: content})
		}
	}

	if finalReply == "" {
		finalReply = "Lo siento, no pude completar la operación. Intenta de nuevo."
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"reply":  finalReply,
		"action": action,
	})
}

func (h *Handler) ConfirmPending(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())
	var req pendingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return
	}
	if err := h.service.resolvePending(r.Context(), userID, req.PendingID, "confirm"); err != nil {
		respond.Error(w, http.StatusBadRequest, "PENDING_TRANSFER", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (h *Handler) CancelPending(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())
	var req pendingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return
	}
	if err := h.service.resolvePending(r.Context(), userID, req.PendingID, "cancel"); err != nil {
		respond.Error(w, http.StatusBadRequest, "PENDING_TRANSFER", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"status": "voided"})
}

// executeTool ejecuta una herramienta y devuelve el contenido que verá el modelo
// (string), una acción opcional para la UI y un error.
func (s *Service) executeTool(ctx context.Context, userID, name string, args json.RawMessage) (string, *Action, error) {
	switch name {
	case "get_balances":
		return s.toolBalances(ctx, userID)
	case "create_pending_transfer":
		return s.toolCreatePending(ctx, userID, args)
	case "confirm_pending_transfer":
		return s.toolConfirmPending(ctx, userID, args)
	case "cancel_pending_transfer":
		return s.toolCancelPending(ctx, userID, args)
	default:
		return "", nil, fmt.Errorf("herramienta desconocida %q", name)
	}
}

func (s *Service) toolBalances(ctx context.Context, userID string) (string, *Action, error) {
	accs, err := s.accounts.ListByUser(ctx, userID)
	if err != nil {
		return "", nil, err
	}

	ids := make([]tigerbeetle.AccountID, 0, len(accs))
	for _, a := range accs {
		id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}

	type bal struct {
		AccountNumber string `json:"account_number"`
		AccountType   string `json:"account_type"`
		BalanceCents  int64  `json:"balance_cents"`
	}
	list := make([]bal, 0, len(accs))
	if len(ids) > 0 {
		views, err := s.ledger.Balances(ctx, ids)
		if err != nil {
			return "", nil, err
		}
		byID := make(map[tigerbeetle.AccountID]int64, len(views))
		for _, v := range views {
			byID[v.AccountID] = v.BalanceCents
		}
		for _, a := range accs {
			id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
			if err != nil {
				continue
			}
			list = append(list, bal{AccountNumber: a.AccountNumber, AccountType: a.AccountType, BalanceCents: byID[id]})
		}
	}

	out, err := json.Marshal(list)
	return string(out), nil, err
}

type createPendingArgs struct {
	FromAccount string  `json:"from_account"`
	ToAccount   string  `json:"to_account"`
	Amount      float64 `json:"amount"`
	Description string  `json:"description"`
}

func (s *Service) toolCreatePending(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error) {
	var a createPendingArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", nil, errors.New("argumentos inválidos para create_pending_transfer")
	}
	a.FromAccount = strings.TrimSpace(a.FromAccount)
	a.ToAccount = strings.TrimSpace(a.ToAccount)
	if a.FromAccount == "" || a.ToAccount == "" || a.Amount <= 0 {
		return "", nil, errors.New("from_account, to_account y amount > 0 son obligatorios")
	}
	if a.FromAccount == a.ToAccount {
		return "", nil, errors.New("origen y destino no pueden ser iguales")
	}

	amountCents := int64(math.Round(a.Amount * 100))

	ok, err := s.accounts.OwnedBy(ctx, userID, a.FromAccount)
	if err != nil || !ok {
		return "", nil, errors.New("la cuenta de origen no existe o no es tuya")
	}

	destTB := s.ledger.ExternalID()
	if a.ToAccount != externalAccount {
		exists, err := s.txs.Exists(ctx, a.ToAccount)
		if err != nil {
			return "", nil, err
		}
		if !exists {
			return "", nil, errors.New("la cuenta destino no existe")
		}
		parsed, err := tigerbeetle.AccountIDFromNumber(a.ToAccount)
		if err != nil {
			return "", nil, errors.New("cuenta destino inválida")
		}
		destTB = parsed
	}

	fromTB, err := tigerbeetle.AccountIDFromNumber(a.FromAccount)
	if err != nil {
		return "", nil, errors.New("cuenta de origen inválida")
	}

	views, err := s.ledger.Balances(ctx, []tigerbeetle.AccountID{fromTB})
	if err != nil {
		return "", nil, errors.New("no pude consultar el saldo")
	}
	if len(views) == 0 || amountCents > views[0].BalanceCents {
		return "", nil, errors.New("saldo insuficiente")
	}

	pendingTB, err := s.ledger.CreatePending(ctx, fromTB, destTB, uint64(amountCents))
	if err != nil {
		return "", nil, errors.New("no se pudo crear el movimiento pendiente")
	}

	pendingID := pendingTB.String()
	if err := s.pendings.Create(ctx, PendingTransfer{
		UserID:      userID,
		PendingID:   pendingID,
		FromAccount: a.FromAccount,
		ToAccount:   a.ToAccount,
		AmountCents: amountCents,
		Description: a.Description,
	}); err != nil {
		return "", nil, err
	}

	action := &Action{
		Type:        "pending_transfer",
		PendingID:   pendingID,
		FromAccount: a.FromAccount,
		ToAccount:   a.ToAccount,
		AmountCents: amountCents,
		Description: a.Description,
	}
	return fmt.Sprintf("Movimiento pendiente creado con pending_id %s por %s de %s a %s. Espera la confirmación del usuario.",
		pendingID, centsToDollars(amountCents), a.FromAccount, a.ToAccount), action, nil
}

type pendingArgs struct {
	PendingID string `json:"pending_id"`
}

func (s *Service) toolConfirmPending(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error) {
	var a pendingArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", nil, errors.New("argumentos inválidos")
	}
	if err := s.resolvePending(ctx, userID, a.PendingID, "confirm"); err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Transferencia %s confirmada y aplicada.", a.PendingID), nil, nil
}

func (s *Service) toolCancelPending(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error) {
	var a pendingArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", nil, errors.New("argumentos inválidos")
	}
	if err := s.resolvePending(ctx, userID, a.PendingID, "cancel"); err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Transferencia %s cancelada.", a.PendingID), nil, nil
}

func (s *Service) resolvePending(ctx context.Context, userID, pendingID, mode string) error {
	p, err := s.pendings.ByPendingID(ctx, pendingID)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return ErrPendingNotYours
	}
	if p.Status != "pending" {
		return ErrPendingProcessed
	}

	fromTB, err := tigerbeetle.AccountIDFromNumber(p.FromAccount)
	if err != nil {
		return errors.New("cuenta de origen inválida")
	}
	destTB := s.ledger.ExternalID()
	if p.ToAccount != externalAccount {
		parsed, err := tigerbeetle.AccountIDFromNumber(p.ToAccount)
		if err != nil {
			return errors.New("cuenta destino inválida")
		}
		destTB = parsed
	}
	pendingTB, err := tigerbeetle.HexToID(p.PendingID)
	if err != nil {
		return errors.New("pending_id inválido")
	}

	if mode == "confirm" {
		if err := s.ledger.PostPending(ctx, pendingTB, fromTB, destTB, uint64(p.AmountCents)); err != nil {
			if !errors.Is(err, tigerbeetle.ErrExists) && !errors.Is(err, tigerbeetle.ErrAlreadyPosted) {
				return err
			}
		}
		txType := "transfer"
		if p.ToAccount == externalAccount {
			txType = "withdrawal"
		}
		if _, err := s.txs.Create(ctx, transactions.Transaction{
			FromAccount: p.FromAccount,
			ToAccount:   p.ToAccount,
			Type:        txType,
			AmountCents: p.AmountCents,
			Description: p.Description,
			Status:      "completed",
		}); err != nil {
			return err
		}
		return s.pendings.SetStatus(ctx, pendingID, "completed")
	}

	if err := s.ledger.VoidPending(ctx, pendingTB, fromTB, destTB, uint64(p.AmountCents)); err != nil {
		if !errors.Is(err, tigerbeetle.ErrExists) && !errors.Is(err, tigerbeetle.ErrAlreadyVoided) {
			return err
		}
	}
	return s.pendings.SetStatus(ctx, pendingID, "voided")
}

func centsToDollars(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}
