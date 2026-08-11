package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/db"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/seed"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
)

func main() {
	dataPath := flag.String("data", "cmd/seed/data/datos-prueba-HNL.json", "ruta del JSON de datos de prueba")
	runMigrations := flag.Bool("migrate", true, "aplicar migraciones antes de sembrar")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	if *runMigrations {
		migDir := "migrations"
		if _, err := os.Stat(migDir); err != nil {
			migDir = "backend/migrations"
		}
		if err := db.Migrate(ctx, pool, migDir); err != nil {
			log.Fatalf("migraciones: %v", err)
		}
	}

	client, err := tigerbeetle.NewClient(cfg.TBClusterID, cfg.TBAddress, cfg.TBExternalAccountID)
	if err != nil {
		log.Fatalf("tigerbeetle: %v", err)
	}
	defer client.Close()

	res, err := seed.Run(ctx, pool, client, *dataPath)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	log.Printf("usuarios: %d creados, %d existentes", res.UsersCreated, res.UsersSkipped)
	log.Printf("cuentas: %d creadas, %d existentes", res.AccountsCreated, res.AccountsSkipped)
	log.Printf("transacciones: %d aplicadas, %d existentes", res.TransactionsReplayed, res.TransactionsExisting)

	if res.BalanceVerificationOK {
		log.Printf("verificación: todos los balances coinciden")
	} else {
		log.Printf("verificación: %d cuentas NO coinciden", res.BalanceMismatchCount)
		for _, m := range res.BalanceMismatches {
			log.Printf("  %s: esperado %.2f, real %.2f", m.Number, m.Expected, m.Real)
		}
	}
}

