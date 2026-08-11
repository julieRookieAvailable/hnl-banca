package respond

import (
	"encoding/json"
	"net/http"
)

// ErrorDetail es el formato único de error JSON de toda la API:
//
//	{"error": {"code": "insufficient_funds", "message": "..."}}
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// JSONRaw escribe un body JSON ya codificado (p. ej. respuestas en cache de
// idempotencia) sin volver a serializarlo.
func JSONRaw(w http.ResponseWriter, status int, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if len(raw) > 0 {
		_, _ = w.Write(raw)
	}
}

func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}
