package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
)

// statusRecorder captura el código de estado para los logs.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// WithRequestID asigna un request_id y lo guarda en el contexto para propagarlo.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logging registra cada petición con slog: método, path, status, duración,
// request_id y (cuando aplica) user_id enmascarado.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestID(r.Context()),
				"user_id", maskUserID(UserID(r.Context())),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// maskUserID enmascara el id del usuario en los logs (ej. "ab12***").
func maskUserID(id string) string {
	if id == "" {
		return "-"
	}
	if len(id) <= 4 {
		return "***"
	}
	return id[:4] + "***"
}

// Timeout aplica un timeout de contexto a cada petición.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			done := make(chan struct{})
			go func() {
				next.ServeHTTP(w, r.WithContext(ctx))
				close(done)
			}()
			select {
			case <-done:
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					respond.Error(w, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "la petición tardó demasiado")
				}
			}
		})
	}
}
