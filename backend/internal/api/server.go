package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/auth"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/chat"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/idempotency"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/middleware"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/transactions"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/users"
)

// Server agrupa el handler HTTP y los recursos que deben cerrarse al apagar.
type Server struct {
	handler http.Handler
	ledger  *tigerbeetle.Client
}

func NewServer(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*Server, error) {
	ledger, err := tigerbeetle.NewClient(cfg.TBClusterID, cfg.TBAddress, cfg.TBExternalAccountID)
	if err != nil {
		return nil, err
	}

	userRepo := users.NewPostgresUserRepository(pool)
	accountRepo := accounts.NewPostgresAccountRepository(pool)
	authService := auth.NewService(cfg, userRepo, pool, accounts.NewOpener(accountRepo, ledger))
	authHandler := auth.NewHandler(authService)

	accountsHandler := accounts.NewHandler(cfg, accountRepo, ledger)

	txRepo := transactions.NewPostgresTransactionRepository(pool)
	transferSvc := transactions.NewService(accountRepo, txRepo, ledger)
	transferHandler := transactions.NewHandler(transferSvc, idempotency.NewPostgresStore(pool))

	var chatProvider chat.ChatProvider
	if cfg.OpenRouterAPIKey != "" {
		chatProvider = chat.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
	}
	chatHandler := chat.NewHandler(chat.NewService(accountRepo, ledger, txRepo,
		chat.NewPostgresPendingStore(pool), chatProvider, logger))

	auth := func(next http.Handler) http.Handler { return middleware.RequireAuth(cfg, next) }

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.Handle("POST /auth/register",
		middleware.RateLimit(middleware.NewRateLimiter(2, 5))(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /auth/login",
		middleware.RateLimit(middleware.NewRateLimiter(5, 10))(http.HandlerFunc(authHandler.Login)))
	mux.HandleFunc("POST /auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /auth/logout", authHandler.Logout)

	mux.Handle("GET /accounts", auth(http.HandlerFunc(accountsHandler.List)))
	mux.Handle("GET /accounts/{accountNumber}", auth(http.HandlerFunc(accountsHandler.Get)))
	mux.Handle("GET /accounts/{accountNumber}/transactions", auth(http.HandlerFunc(transferHandler.ListByAccount)))
	mux.Handle("GET /transactions/recent", auth(http.HandlerFunc(transferHandler.ListRecent)))
	mux.Handle("POST /transfers", auth(http.HandlerFunc(transferHandler.Transfer)))

	mux.Handle("POST /chat", auth(http.HandlerFunc(chatHandler.Chat)))
	mux.Handle("POST /chat/confirm", auth(http.HandlerFunc(chatHandler.ConfirmPending)))
	mux.Handle("POST /chat/cancel", auth(http.HandlerFunc(chatHandler.CancelPending)))
	handler := middleware.Timeout(cfg.RequestTimeout)(
		middleware.Logging(logger)(
			middleware.WithRequestID(middleware.CORS(cfg.CORSOrigin)(mux)),
		),
	)

	return &Server{handler: handler, ledger: ledger}, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) Close() error {
	if s.ledger != nil {
		s.ledger.Close()
	}
	return nil
}
