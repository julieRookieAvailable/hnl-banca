// Package seed carga el dataset de prueba de forma idempotente: re-ejecutar
// el seed no duplica usuarios, cuentas ni movimientos, tanto en Postgres
// (claves únicas + ON CONFLICT) como en TigerBeetle (ids deterministas
// tolerantes a Exists). Es usado por el binario cmd/seed y por la API en el
// arranque (SEED_ON_START).
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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

type Mismatch struct {
	Number   string
	Expected float64
	Real     float64
}

type Result struct {
	UsersCreated          int
	UsersSkipped          int
	AccountsCreated       int
	AccountsSkipped       int
	TransactionsReplayed  int
	TransactionsExisting  int
	BalanceMismatchCount  int
	BalanceMismatches     []Mismatch
	BalanceVerificationOK bool
}

// ResolveDataPath acepta una ruta relativa al repo y la valida desde el CWD
// actual o desde la raíz del repo (backend/<path>).
func ResolveDataPath(dataPath string) string {
	if _, err := os.Stat(dataPath); err == nil {
		return dataPath
	}
	if _, err := os.Stat("backend/" + dataPath); err == nil {
		return "backend/" + dataPath
	}
	return dataPath
}

// Run carga el dataset y deja Postgres y TigerBeetle en un estado consistente.
// Es idempotente: puede llamarse en cada arranque sin efectos secundarios.
func Run(ctx context.Context, pool *pgxpool.Pool, client *tigerbeetle.Client, dataPath string) (Result, error) {
	var res Result

	path := ResolveDataPath(dataPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("leyendo datos: %w", err)
	}
	var data seedData
	if err := json.Unmarshal(raw, &data); err != nil {
		return res, fmt.Errorf("parseando datos: %w", err)
	}
	log.Printf("seed: datos en %s: %d usuarios, %d cuentas, %d transacciones",
		path, len(data.Users), len(data.Accounts), len(data.Transactions))

	// 1. Cuenta EXTERNAL (idempotente en TB).
	if err := client.CreateAccounts(ctx, []tigerbeetle.AccountSpec{{ID: client.ExternalID(), Code: tigerbeetle.CodeExternal}}); err != nil {
		return res, fmt.Errorf("cuenta external: %w", err)
	}

	// 2. Usuarios (password bcrypt, idempotente por id).
	for _, u := range data.Users {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, u.ID).Scan(&exists); err != nil {
			return res, fmt.Errorf("consultando usuario %s: %w", u.ID, err)
		}
		if exists {
			res.UsersSkipped++
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err != nil {
			return res, fmt.Errorf("hasheando %s: %w", u.Email, err)
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
					return res, fmt.Errorf("insertando usuario %s: %w", u.Email, err)
				}
				log.Printf("seed: email duplicado en dataset, usuario %s insertado como %s", u.ID, altEmail)
				res.UsersCreated++
				continue
			}
			return res, fmt.Errorf("insertando usuario %s: %w", u.Email, err)
		}
		res.UsersCreated++
	}

	// 3. Cuentas: Postgres + TigerBeetle (ambos idempotentes).
	tbIDByNumber := make(map[string]tb.Uint128)
	byNumber := make(map[string]seedAccount)
	for _, a := range data.Accounts {
		id, err := tigerbeetle.AccountIDFromNumber(a.AccountNumber)
		if err != nil {
			return res, fmt.Errorf("id de cuenta inválido %s: %w", a.AccountNumber, err)
		}
		if prev, dup := tbIDByNumber[a.AccountNumber]; dup && prev != id {
			return res, fmt.Errorf("colisión de id tb para %s", a.AccountNumber)
		}
		tbIDByNumber[a.AccountNumber] = id
		byNumber[a.AccountNumber] = a
	}

	for _, a := range data.Accounts {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM bank_accounts WHERE account_number = $1)`, a.AccountNumber).Scan(&exists); err != nil {
			return res, fmt.Errorf("consultando cuenta %s: %w", a.AccountNumber, err)
		}
		if exists {
			res.AccountsSkipped++
		} else {
			id := tbIDByNumber[a.AccountNumber]
			if _, err := pool.Exec(ctx,
				`INSERT INTO bank_accounts (account_number, user_id, tb_account_id, account_type, currency)
				 VALUES ($1,$2,$3,$4,$5)`,
				a.AccountNumber, a.UserID, idToInt64(id), a.AccountType, a.Currency); err != nil {
				return res, fmt.Errorf("insertando cuenta %s: %w", a.AccountNumber, err)
			}
			res.AccountsCreated++
		}
		if err := client.CreateAccounts(ctx, []tigerbeetle.AccountSpec{
			{ID: tbIDByNumber[a.AccountNumber], Code: tigerbeetle.CodeForType(a.AccountType)},
		}); err != nil {
			return res, fmt.Errorf("creando cuenta tb %s: %w", a.AccountNumber, err)
		}
	}

	// 4. Depósitos iniciales: EXTERNAL -> cuenta por initial_balance (idempotente en TB).
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
		if err != nil && !errors.Is(err, tigerbeetle.ErrExists) {
			return res, fmt.Errorf("depósito inicial %s: %w", a.AccountNumber, err)
		}
	}

	// 5. Replay de transacciones en orden cronológico (idempotente en ambos lados).
	replay := make([]seedTransaction, len(data.Transactions))
	copy(replay, data.Transactions)
	sort.SliceStable(replay, func(i, j int) bool {
		return replay[i].Timestamp.Before(replay[j].Timestamp)
	})

	for i, t := range replay {
		if err := applyTransaction(ctx, pool, client, byNumber, tbIDByNumber, t, uint64(i+1)); err != nil {
			if errors.Is(err, tigerbeetle.ErrExists) {
				res.TransactionsExisting++
				continue
			}
			return res, fmt.Errorf("transacción #%d (%s -> %s): %w", i, t.FromAccount, t.ToAccount, err)
		}
		res.TransactionsReplayed++
	}

	// 6. Verificación de balances.
	res.BalanceMismatches = verifyBalances(ctx, pool, client, byNumber, initialCents)
	res.BalanceMismatchCount = len(res.BalanceMismatches)
	res.BalanceVerificationOK = res.BalanceMismatchCount == 0

	return res, nil
}

// applyTransaction registra primero en Postgres (ON CONFLICT por tb_transfer_id)
// y luego en TigerBeetle con id determinista; si la transferencia TB ya existe
// devuelve tigerbeetle.ErrExists para que el llamador la considere aplicada.
func applyTransaction(ctx context.Context, pool *pgxpool.Pool, client *tigerbeetle.Client, byNumber map[string]seedAccount, tbID map[string]tb.Uint128, t seedTransaction, id uint64) error {
	debit, credit, err := resolveDirection(client, t, tbID)
	if err != nil {
		return err
	}
	cents := uint64(dollarsToCents(t.Amount))
	if cents == 0 {
		return nil
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO transactions (from_account, to_account, type, amount, description, timestamp, status, tb_transfer_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (tb_transfer_id) DO NOTHING`,
		t.FromAccount, t.ToAccount, t.Type, t.Amount, t.Description, t.Timestamp, t.Status, id); err != nil {
		return err
	}

	err = client.CreateTransfers(ctx, []tigerbeetle.TransferSpec{{
		ID:              tb.ToUint128(id),
		DebitAccountID:  debit,
		CreditAccountID: credit,
		AmountCents:     cents,
	}})
	if err != nil {
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

func verifyBalances(ctx context.Context, pool *pgxpool.Pool, client *tigerbeetle.Client, byNumber map[string]seedAccount, initialCents map[string]int64) []Mismatch {
	var result []Mismatch
	for number := range byNumber {
		var netSum int64
		err := pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(CASE WHEN from_account = $1 THEN -amount ELSE amount END), 0) * 100
			 FROM transactions
			 WHERE from_account = $1 OR to_account = $1`,
			number).Scan(&netSum)
		if err != nil {
			log.Printf("seed: error calculando neto de %s: %v", number, err)
			continue
		}
		expected := initialCents[number] + netSum

		id, err := tigerbeetle.AccountIDFromNumber(number)
		if err != nil {
			continue
		}
		real, err := client.Balance(ctx, id)
		if err != nil {
			log.Printf("seed: error consultando balance de %s: %v", number, err)
			continue
		}
		if expected != real {
			result = append(result, Mismatch{Number: number, Expected: float64(expected) / 100, Real: float64(real) / 100})
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
		return 0
	}
	return int64(lo)
}
