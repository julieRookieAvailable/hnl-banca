package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/api"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config inválida", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("no se pudo conectar a postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	migDir := "migrations"
	if _, err := os.Stat(migDir); err != nil {
		migDir = "backend/migrations"
	}
	if err := db.Migrate(ctx, pool, migDir); err != nil {
		logger.Error("fallaron las migraciones", "error", err)
		os.Exit(1)
	}
	logger.Info("base de datos lista")

	srv, err := api.NewServer(ctx, cfg, pool, logger)
	if err != nil {
		logger.Error("no se pudo crear la api", "error", err)
		os.Exit(1)
	}
	defer srv.Close()

	httpServer := &http.Server{
		Addr:              cfg.APIHost + ":" + cfg.APIPort,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api escuchando", "addr", "http://"+httpServer.Addr)
		errCh <- httpServer.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("señal recibida, apagando", "signal", sig.String())
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("servidor terminó con error", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("apagado del servidor http", "error", err)
	}
	logger.Info("servidor detenido")
}
