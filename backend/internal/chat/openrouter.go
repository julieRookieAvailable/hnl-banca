// Package chat implementa el asistente de banca por chat: conecta a un proveedor
// de LLM con function calling (OpenRouter por defecto) y expone herramientas para
// consultar saldos y gestionar transferencias en dos fases.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// Tipos neutros de mensajes: no dependen del wire format de OpenRouter.

type ToolCall struct {
	ID   string
	Name string
	Args json.RawMessage
}

type Message struct {
	Role       string
	Content    any
	ToolCallID string
	ToolCalls  []ToolCall
}

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type Result struct {
	Content   string
	ToolCalls []ToolCall
}

// ChatProvider es el contrato del proveedor de IA. La implementación concreta
// vive en openrouter.go y cualquier alternativa (Anthropic, etc.) puede
// implementar la misma interfaz sin tocar handlers ni servicios.
type ChatProvider interface {
	Complete(ctx context.Context, messages []Message, tools []Tool) (*Result, error)
}

// openRouterClient implementa ChatProvider sobre la API compatible de OpenRouter.
type openRouterClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewOpenRouterClient(apiKey, model string) *openRouterClient {
	return &openRouterClient{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 90 * time.Second},
	}
}

type orToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type orMessage struct {
	Role       string       `json:"role"`
	Content    any          `json:"content,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []orToolCall `json:"tool_calls,omitempty"`
}

type orFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type orTool struct {
	Type     string     `json:"type"`
	Function orFunction `json:"function"`
}

type orResponse struct {
	Choices []struct {
		Message struct {
			Role      string       `json:"role"`
			Content   any          `json:"content"`
			ToolCalls []orToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *openRouterClient) Complete(ctx context.Context, messages []Message, tools []Tool) (*Result, error) {
	wireMsgs := make([]orMessage, 0, len(messages))
	for _, m := range messages {
		or := orMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			or.ToolCalls = append(or.ToolCalls, orToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{Name: tc.Name, Arguments: tc.Args},
			})
		}
		wireMsgs = append(wireMsgs, or)
	}
	wireTools := make([]orTool, 0, len(tools))
	for _, t := range tools {
		wireTools = append(wireTools, orTool{
			Type: "function",
			Function: orFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	payload := map[string]any{
		"model":    c.model,
		"messages": wireMsgs,
		"tools":    wireTools,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://localhost:8080")
	req.Header.Set("X-Title", "Banca en Linea HNL")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var or orResponse
	if err := json.Unmarshal(respBody, &or); err != nil {
		return nil, err
	}
	if or.Error != nil {
		return nil, fmt.Errorf("openrouter: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return nil, fmt.Errorf("openrouter: sin respuestas")
	}

	msg := or.Choices[0].Message
	result := &Result{Content: fmt.Sprintf("%v", msg.Content)}
	for _, tc := range msg.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: tc.Function.Arguments,
		})
	}
	return result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
