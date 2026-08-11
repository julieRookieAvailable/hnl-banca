package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/db"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/tigerbeetle"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	"golang.org/x/crypto/bcrypt"
)

type seedUser struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Password  string    `json:"password"`
	FullName  string    `json:"full_name"`
	CreatedAt time.Time `json:"created_at"`
}

type seedAccount struct {
	AccountNumber  string  `json:"account_number"`
	UserID         string  `json:"user_id"`
	InitialBalance float64 `json:"initial_balance"`
	Currency       string  `json:"currency"`
	AccountType    string  `json:"account_type"`
}

type seedTransaction struct {
	FromAccount string    `json:"from_account"`
	ToAccount   string    `json:"to_account"`
	Amount      float64   `json:"amount"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	Status      string    `json:"status"`
}

type seedData struct {
	Users        []seedUser        `json:"users"`
	Accounts     []seedAccount     `json:"accounts"`
	Transactions []seedTransaction `json:"transactions"`
}

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

	raw, err := os.ReadFile(*dataPath)
	if err != nil {
		log.Fatalf("leyendo datos: %v", err)
	}
	var data seedData
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Fatalf("parseando datos: %v", err)
	}
	log.Printf("datos: %d usuarios, %d cuentas, %d transacciones",
		len(data.Users), len(data.Accounts), len(data.Transactions))

	// 1. Cuenta EXTERNAL
	if err := client.CreateAccounts(ctx, []tigerbeetle.AccountSpec{{ID: client.ExternalID(), Code: tigerbeetle.CodeExternal}}); err != nil {
		log.Fatalf("cuenta external: %v", err)
	}

	// 2. Usuarios (password bcrypt, idempotente)
	createdUsers, skippedUsers := 0, 0
	for i, u := range data.Users {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, u.ID).Scan(&exists); err != nil {
			log.Fatalf("consultando usuario %s: %v", u.ID, err)
		}
		if exists {
			skippedUsers++
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("hasheando %s: %v", u.Email, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, password_hash, full_name, created_at) VALUES ($1,$2,$3,$4,$5)`,
			u.ID, u.Email, string(hash), u.FullName, u.CreatedAt); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				// El dataset trae emails repetidos con ids distintos: para no romper
				// el FK de sus cuentas, se inserta el usuario con un email único
				// derivado del id (el original ya lo usa otro usuario).
				altEmail := uniqueEmail(u.Email, u.ID)
				if _, err := pool.Exec(ctx,
					`INSERT INTO users (id, email, password_hash, full_name, created_at) VALUES ($1,$2,$3,$4,$5)`,
					u.ID, altEmail, string(hash), u.FullName, u.CreatedAt); err != nil {
					log.Fatalf("insertando usuario %s: %v", u.Email, err)
				}
				log.Printf("  email duplicado en dataset, usuario %s insertado como %s", u.ID, altEmail)
				createdUsers++
				continue
			}
			log.Fatalf("insertando usuario %s: %v", u.Email, err)
		}
		createdUsers++
		if (i+1)%200 == 0 {
			log.Printf("  usuarios procesados: %d", i+1)
		}
	}
	log.Printf("usuarios: %d creados, %d existentes", createdUsers, skippedUsers)

	// 3. Cuentas: Postgres + TigerBeetle
	tbIDByNumber := make(map[string]tb.Uint128)
	byNumber := make(map[string]seedAccount)
	for _, a := range data.Accounts {
		id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
		if err != nil {
			log.Fatalf("id de cuenta inválido %s: %v", a.AccountNumber, err)
		}
		if prev, dup := tbIDByNumber[a.AccountNumber]; dup && prev != id {
			log.Fatalf("colisión de id tb para %s", a.AccountNumber)
		}
		tbIDByNumber[a.AccountNumber] = id
		byNumber[a.AccountNumber] = a
	}

	createdAccounts, existingAccounts := 0, 0
	for _, a := range data.Accounts {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM bank_accounts WHERE account_number = $1)`, a.AccountNumber).Scan(&exists); err != nil {
			log.Fatalf("consultando cuenta %s: %v", a.AccountNumber, err)
		}
		if exists {
			existingAccounts++
		} else {
			id := tbIDByNumber[a.AccountNumber]
			if _, err := pool.Exec(ctx,
				`INSERT INTO bank_accounts (account_number, user_id, tb_account_id, account_type, currency)
				 VALUES ($1,$2,$3,$4,$5)`,
				a.AccountNumber, a.UserID, idToInt64(id), a.AccountType, a.Currency); err != nil {
				log.Fatalf("insertando cuenta %s: %v", a.AccountNumber, err)
			}
			createdAccounts++
		}
		if err := client.CreateAccounts(ctx, []tigerbeetle.AccountSpec{
			{ID: tbIDByNumber[a.AccountNumber], Code: tigerbeetle.CodeForType(a.AccountType)},
		}); err != nil {
			log.Fatalf("creando cuenta tb %s: %v", a.AccountNumber, err)
		}
	}
	log.Printf("cuentas: %d creadas, %d existentes", createdAccounts, existingAccounts)

	// 4. Depósitos iniciales: EXTERNAL -> cuenta por initial_balance
	initialCents := make(map[string]int64)
	for i, a := range data.Accounts {
		cents := dollarsToCents(a.InitialBalance)
		initialCents[a.AccountNumber] = cents
		if cents <= 0 {
			continue
		}
		err := client.CreateTransfers(ctx, []tigerbeetle.TransferSpec{{
			ID:              tb.ToUint128(uint64(1_000_000 + i)),
			DebitAccountID:  client.ExternalID(),
			CreditAccountID: tbIDByNumber[a.AccountNumber],
			AmountCents:     uint64(cents),
		}})
		if err != nil && err != tigerbeetle.ErrExists {
			log.Fatalf("depósito inicial %s: %v", a.AccountNumber, err)
		}
	}
	log.Printf("depósitos iniciales listos")

	// 5. Replay de transacciones en orden cronológico
	replay := make([]seedTransaction, len(data.Transactions))
	copy(replay, data.Transactions)
	sort.SliceStable(replay, func(i, j int) bool {
		return replay[i].Timestamp.Before(replay[j].Timestamp)
	})

	replayed, existingTx := 0, 0
	for i, t := range replay {
		if err := applyTransaction(ctx, pool, client, byNumber, tbIDByNumber, t, uint64(i+1)); err != nil {
			if err == tigerbeetle.ErrExists {
				existingTx++
				continue
			}
			log.Fatalf("transacción #%d (%s -> %s): %v", i, t.FromAccount, t.ToAccount, err)
		}
		replayed++
	}
	log.Printf("transacciones: %d aplicadas, %d existentes", replayed, existingTx)

	// 6. Verificación de balances
	log.Printf("--- verificación de balances ---")
	mismatches := verifyBalances(ctx, pool, client, byNumber, initialCents)
	if len(mismatches) == 0 {
		log.Printf("verificación: todos los balances coinciden ✔")
	} else {
		log.Printf("verificación: %d cuentas NO coinciden", len(mismatches))
		for _, m := range mismatches {
			log.Printf("  %s: esperado %.2f, real %.2f", m.number, m.expected, m.real)
		}
	}
}

func applyTransaction(ctx context.Context, pool *pgxpool.Pool, client *tigerbeetle.Client, byNumber map[string]seedAccount, tbID map[string]tb.Uint128, t seedTransaction, id uint64) error {
	debit, credit, err := resolveDirection(client, t, tbID)
	if err != nil {
		return err
	}
	cents := uint64(dollarsToCents(t.Amount))
	if cents == 0 {
		return nil
	}

	txID := tb.ToUint128(id)
	err = client.CreateTransfers(ctx, []tigerbeetle.TransferSpec{{
		ID:              txID,
		DebitAccountID:  debit,
		CreditAccountID: credit,
		AmountCents:     cents,
	}})
	if err != nil {
		return err
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO transactions (from_account, to_account, type, amount, description, timestamp, status, tb_transfer_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.FromAccount, t.ToAccount, t.Type, t.Amount, t.Description, t.Timestamp, t.Status, id); err != nil {
		return err
	}
	return nil
}

// resolveDirection decide debita/crédita en TigerBeetle para el tipo de transacción.
func resolveDirection(client *tigerbeetle.Client, t seedTransaction, tbID map[string]tb.Uint128) (tb.Uint128, tb.Uint128, error) {
	external := client.ExternalID()
	lookup := func(number string) (tb.Uint128, error) {
		if number == "EXTERNAL" {
			return external, nil
		}
		if id, ok := tbID[number]; ok {
			return id, nil
		}
		return tb.Uint128{}, fmt.Errorf("cuenta desconocida %q", number)
	}

	switch t.Type {
	case "deposit":
		credit, err := lookup(t.ToAccount)
		return external, credit, err
	case "withdrawal":
		debit, err := lookup(t.FromAccount)
		return debit, external, err
	case "transfer", "internal_transfer":
		debit, err := lookup(t.FromAccount)
		if err != nil {
			return tb.Uint128{}, tb.Uint128{}, err
		}
		credit, err := lookup(t.ToAccount)
		return debit, credit, err
	default:
		return tb.Uint128{}, tb.Uint128{}, fmt.Errorf("tipo de transacción desconocido %q", t.Type)
	}
}

type mismatch struct {
	number   string
	expected float64
	real     float64
}

func verifyBalances(ctx context.Context, pool *pgxpool.Pool, client *tigerbeetle.Client, byNumber map[string]seedAccount, initialCents map[string]int64) []mismatch {
	var result []mismatch
	for number := range byNumber {
		var netSum int64
		err := pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(CASE WHEN from_account = $1 THEN -amount ELSE amount END), 0) * 100
			 FROM transactions
			 WHERE from_account = $1 OR to_account = $1`,
			number).Scan(&netSum)
		if err != nil {
			log.Printf("  error calculando neto de %s: %v", number, err)
			continue
		}
		expected := initialCents[number] + netSum

		id, err := tigerbeetle.AccountIDFromNumber(number)
		if err != nil {
			continue
		}
		real, err := client.Balance(ctx, id)
		if err != nil {
			log.Printf("  error consultando balance de %s: %v", number, err)
			continue
		}
		if expected != real {
			result = append(result, mismatch{number: number, expected: float64(expected) / 100, real: float64(real) / 100})
		}
	}
	return result
}

func dollarsToCents(v float64) int64 {
	return int64(math.Round(v * 100))
}

// uniqueEmail convierte un email duplicado en uno único para no violar el UNIQUE.
func uniqueEmail(email, userID string) string {
	at := strings.IndexByte(email, '@')
	short := strings.ReplaceAll(userID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	if at < 0 {
		return email + "+" + short
	}
	return email[:at] + "+" + short + email[at:]
}

func idToInt64(id tb.Uint128) int64 {
	lo, hi := id.Uint64()
	if hi != 0 {
		log.Fatalf("id tb fuera de rango int64")
	}
	return int64(lo)
}
