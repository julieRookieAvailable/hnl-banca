package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
)

// RateLimiter es un limitador en memoria por clave (IP o ruta+IP) basado en
// token bucket. Suficiente para la API de un solo nodo.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64
	burst    int
	lastGC   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(ratePerSec float64, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    ratePerSec,
		burst:   burst,
		lastGC:  time.Now(),
	}
}

// Allow consume un token para la clave dada y devuelve true si está permitido.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.gc(now)

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: float64(l.burst), last: now}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gc elimina buckets inactivos para no acumular memoria.
func (l *RateLimiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// RateLimit protege un handler por IP.
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(r.RemoteAddr) {
				respond.Error(w, http.StatusTooManyRequests, "RATE_LIMITED", "demasiadas peticiones, inténtalo de nuevo")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS configura los encabezados con un origen explícito.
func CORS(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
