package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bankTool describe una herramienta del asistente: el schema que se anuncia al
// LLM (y se registra en el servidor MCP) y la función que la ejecuta enlazada
// al usuario autenticado de la petición.
type bankTool struct {
	name        string
	description string
	parameters  json.RawMessage
	run         func(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error)
}

// toolCatalog es la única fuente de verdad de las herramientas del asistente.
// De aquí salen tanto los tool definitions que ve el proveedor de IA como los
// tools registrados en el servidor MCP del SDK oficial.
func (s *Service) toolCatalog() []bankTool {
	return []bankTool{
		{
			name:        "get_balances",
			description: "Devuelve las cuentas del cliente autenticado con su saldo disponible en centavos.",
			parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			run: func(ctx context.Context, userID string, _ json.RawMessage) (string, *Action, error) {
				return s.toolBalances(ctx, userID)
			},
		},
		{
			name:        "get_recent_transactions",
			description: "Devuelve los últimos movimientos de las cuentas del cliente autenticado, útil para contestar preguntas sobre el historial.",
			parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			run: func(ctx context.Context, userID string, _ json.RawMessage) (string, *Action, error) {
				return s.toolRecentTransactions(ctx, userID)
			},
		},
		{
			name:        "create_pending_transfer",
			description: "Crea una transferencia en estado pendiente (dos fases) que requiere confirmación explícita del usuario. to_account puede ser EXTERNAL.",
			parameters: json.RawMessage(`{
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
			run: func(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error) {
				return s.toolCreatePending(ctx, userID, args)
			},
		},
		{
			name:        "confirm_pending_transfer",
			description: "Confirma y aplica una transferencia pendiente usando su pending_id.",
			parameters:  json.RawMessage(`{"type":"object","properties":{"pending_id":{"type":"string"}},"required":["pending_id"],"additionalProperties":false}`),
			run: func(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error) {
				return s.toolConfirmPending(ctx, userID, args)
			},
		},
		{
			name:        "cancel_pending_transfer",
			description: "Cancela (void) una transferencia pendiente usando su pending_id.",
			parameters:  json.RawMessage(`{"type":"object","properties":{"pending_id":{"type":"string"}},"required":["pending_id"],"additionalProperties":false}`),
			run: func(ctx context.Context, userID string, args json.RawMessage) (string, *Action, error) {
				return s.toolCancelPending(ctx, userID, args)
			},
		},
	}
}

// toolDefinitions devuelve el catálogo en el formato que espera el proveedor de IA.
func (s *Service) toolDefinitions() []Tool {
	cat := s.toolCatalog()
	defs := make([]Tool, 0, len(cat))
	for _, t := range cat {
		defs = append(defs, Tool{Name: t.name, Description: t.description, Parameters: t.parameters})
	}
	return defs
}

// mcpSession es una conexión MCP en memoria entre un cliente y el servidor que
// expone las herramientas del banco, enlazadas al usuario de la petición.
type mcpSession struct {
	server *mcp.ServerSession
	client *mcp.ClientSession
}

func (s *mcpSession) close() {
	if s.client != nil {
		s.client.Close()
	}
	if s.server != nil {
		s.server.Close()
	}
}

// newMCPSession crea un servidor MCP con las herramientas del asistente y le
// conecta un cliente usando el SDK oficial (github.com/modelcontextprotocol/go-sdk).
func (s *Service) newMCPSession(ctx context.Context, userID string) (*mcpSession, error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "hnl-banca", Version: "1.0.0"}, nil)
	s.registerTools(server, userID)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("conectando servidor MCP: %w", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "hnl-banca-client", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		return nil, fmt.Errorf("conectando cliente MCP: %w", err)
	}

	return &mcpSession{server: serverSession, client: clientSession}, nil
}

// registerTools registra el catálogo completo como herramientas MCP.
func (s *Service) registerTools(server *mcp.Server, userID string) {
	for _, t := range s.toolCatalog() {
		t := t
		server.AddTool(&mcp.Tool{Name: t.name, Description: t.description, InputSchema: t.parameters},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				content, action, err := t.run(ctx, userID, req.Params.Arguments)
				res := &mcp.CallToolResult{}
				if err != nil {
					res.IsError = true
					res.Content = []mcp.Content{&mcp.TextContent{Text: "Error: " + err.Error()}}
					return res, nil
				}
				res.Content = []mcp.Content{&mcp.TextContent{Text: content}}
				if action != nil {
					if b, err := json.Marshal(action); err == nil {
						res.StructuredContent = json.RawMessage(b)
					}
				}
				return res, nil
			})
	}
}

// executeTool invoca una herramienta a través del protocolo MCP y devuelve el
// contenido que verá el modelo (string), una acción opcional para la UI y un
// error si la herramienta falló.
func (s *Service) executeTool(ctx context.Context, session *mcpSession, name string, args json.RawMessage) (string, *Action, error) {
	arguments, err := argsToMap(args)
	if err != nil {
		return "", nil, err
	}

	res, err := session.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", name, err)
	}
	if res == nil {
		return "", nil, errors.New("resultado vacío de la herramienta")
	}

	content := textContent(res)
	if res.IsError {
		return content, nil, errors.New(content)
	}

	var action *Action
	if data, ok := structuredBytes(res.StructuredContent); ok {
		if err := json.Unmarshal(data, &action); err != nil {
			return "", nil, fmt.Errorf("respuesta inválida de %s: %w", name, err)
		}
	}
	return content, action, nil
}

// structuredBytes normaliza el StructuredContent de un CallToolResult (any)
// a los bytes JSON que lo componen, tolerando que llegue como RawMessage,
// string o un valor ya decodificado.
func structuredBytes(sc any) ([]byte, bool) {
	switch v := sc.(type) {
	case nil:
		return nil, false
	case json.RawMessage:
		return v, true
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		return b, true
	}
}

// argsToMap normaliza los argumentos de una herramienta a un mapa, tolerando
// que algunos proveedores los envíen como string JSON doblemente codificado.
func argsToMap(raw json.RawMessage) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(trimmed, &m); err == nil {
		return m, nil
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err == nil {
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m, nil
		}
	}
	return nil, errors.New("argumentos inválidos de la herramienta")
}

func textContent(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func (s *Service) toolRecentTransactions(ctx context.Context, userID string) (string, *Action, error) {
	list, err := s.txs.ListRecentByUser(ctx, userID, 5)
	if err != nil {
		return "", nil, err
	}

	type item struct {
		FromAccount string `json:"from_account"`
		ToAccount   string `json:"to_account"`
		Type        string `json:"type"`
		AmountCents int64  `json:"amount_cents"`
		Description string `json:"description"`
		Timestamp   string `json:"timestamp"`
		Status      string `json:"status"`
	}
	out := make([]item, 0, len(list))
	for _, t := range list {
		out = append(out, item{
			FromAccount: t.FromAccount,
			ToAccount:   t.ToAccount,
			Type:        t.Type,
			AmountCents: t.AmountCents,
			Description: t.Description,
			Timestamp:   t.Timestamp.Format(time.RFC3339),
			Status:      t.Status,
		})
	}

	b, err := json.Marshal(out)
	return string(b), nil, err
}
