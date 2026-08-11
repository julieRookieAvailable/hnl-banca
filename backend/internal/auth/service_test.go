package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/accounts"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/users"
)

type fakeUserRepo struct {
	byEmail map[string]*users.User
	byID    map[string]*users.User
	created []*users.User
}

func (f *fakeUserRepo) Create(ctx context.Context, email, passwordHash, fullName string) (*users.User, error) {
	if _, exists := f.byEmail[email]; exists {
		return nil, users.ErrEmailTaken
	}
	u := &users.User{ID: "u-" + email, Email: email, PasswordHash: passwordHash, FullName: fullName}
	f.byEmail[email] = u
	f.byID[u.ID] = u
	f.created = append(f.created, u)
	return u, nil
}

func (f *fakeUserRepo) ByEmail(ctx context.Context, email string) (*users.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return nil, users.ErrNotFound
	}
	return u, nil
}

func (f *fakeUserRepo) ByID(ctx context.Context, id string) (*users.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, users.ErrNotFound
	}
	return u, nil
}

type fakeTokenStore struct {
	tokens map[string]tokenRow
	nextID int
}

type tokenRow struct {
	userID    string
	expiresAt time.Time
	revoked   bool
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: make(map[string]tokenRow)}
}

func (f *fakeTokenStore) Insert(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	f.tokens[tokenHash] = tokenRow{userID: userID, expiresAt: expiresAt}
	return nil
}

func (f *fakeTokenStore) Lookup(ctx context.Context, tokenHash string) (string, time.Time, error) {
	row, ok := f.tokens[tokenHash]
	if !ok || row.revoked {
		return "", time.Time{}, nil
	}
	return row.userID, row.expiresAt, nil
}

func (f *fakeTokenStore) Revoke(ctx context.Context, tokenHash string) error {
	row := f.tokens[tokenHash]
	row.revoked = true
	f.tokens[tokenHash] = row
	return nil
}

type fakeOpener struct {
	opened []accounts.Account
	err    error
}

func (f *fakeOpener) Open(ctx context.Context, userID, accountType, currency string) (accounts.Account, error) {
	if f.err != nil {
		return accounts.Account{}, f.err
	}
	a := accounts.Account{UserID: userID, AccountNumber: "4001-0000-0001-1", AccountType: accountType, Currency: currency}
	f.opened = append(f.opened, a)
	return a, nil
}

func testService() *Service {
	cfg := &config.Config{
		JWTSecret:     "test-secret",
		JWTAccessTTL:  15 * time.Minute,
		JWTRefreshTTL: 720 * time.Hour,
	}
	repo := &fakeUserRepo{byEmail: make(map[string]*users.User), byID: make(map[string]*users.User)}
	return &Service{cfg: cfg, users: repo, tokens: newFakeTokenStore(), opener: &fakeOpener{}}
}

func TestRegisterIssuesTokens(t *testing.T) {
	svc := testService()
	u, tokens, err := svc.Register(context.Background(), "a@b.com", "secret1", "Ana")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.ID == "" {
		t.Fatal("id de usuario vacío")
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("se esperaban tokens")
	}
	if tokens.ExpiresIn != int64((15 * time.Minute).Seconds()) {
		t.Fatalf("expiración incorrecta: %d", tokens.ExpiresIn)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc := testService()
	if _, _, err := svc.Register(context.Background(), "a@b.com", "secret1", "Ana"); err != nil {
		t.Fatalf("primer registro: %v", err)
	}
	_, _, err := svc.Register(context.Background(), "a@b.com", "secret2", "Ana")
	if !errors.Is(err, users.ErrEmailTaken) {
		t.Fatalf("esperaba ErrEmailTaken, got %v", err)
	}
}

func TestRegisterOpensAccount(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret", JWTAccessTTL: 15 * time.Minute, JWTRefreshTTL: 720 * time.Hour}
	repo := &fakeUserRepo{byEmail: make(map[string]*users.User), byID: make(map[string]*users.User)}
	opener := &fakeOpener{}
	svc := &Service{cfg: cfg, users: repo, tokens: newFakeTokenStore(), opener: opener}

	if _, _, err := svc.Register(context.Background(), "c@d.com", "secret1", "Carlos"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(opener.opened) != 1 {
		t.Fatalf("esperaba 1 cuenta abierta, got %d", len(opener.opened))
	}
	a := opener.opened[0]
	if a.UserID != "u-c@d.com" || a.AccountType != "checking" || a.Currency != "USD" {
		t.Fatalf("cuenta abierta inesperada: %+v", a)
	}
}

func TestRegisterFailsWhenAccountCannotOpen(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret", JWTAccessTTL: 15 * time.Minute, JWTRefreshTTL: 720 * time.Hour}
	repo := &fakeUserRepo{byEmail: make(map[string]*users.User), byID: make(map[string]*users.User)}
	opener := &fakeOpener{err: errors.New("tb no disponible")}
	svc := &Service{cfg: cfg, users: repo, tokens: newFakeTokenStore(), opener: opener}

	if _, _, err := svc.Register(context.Background(), "c@d.com", "secret1", "Carlos"); err == nil {
		t.Fatal("esperaba error si la cuenta no se puede abrir")
	}
}

func TestLoginValidAndInvalid(t *testing.T) {
	svc := testService()
	_, _, err := svc.Register(context.Background(), "a@b.com", "secret1", "Ana")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, _, err := svc.Login(context.Background(), "a@b.com", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("password incorrecto: esperaba ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := svc.Login(context.Background(), "a@b.com", "secret1"); err != nil {
		t.Fatalf("login válido falló: %v", err)
	}
}

func TestRefreshRotatesTokens(t *testing.T) {
	svc := testService()
	_, tokens, err := svc.Register(context.Background(), "a@b.com", "secret1", "Ana")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	rotated, err := svc.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.RefreshToken == tokens.RefreshToken {
		t.Fatal("el refresh debe rotar el token")
	}
	// El token anterior ya no sirve.
	if _, err := svc.Refresh(context.Background(), tokens.RefreshToken); err == nil {
		t.Fatal("el token de refresco antiguo no debería seguir válido")
	}
}

func TestRefreshInvalidToken(t *testing.T) {
	svc := testService()
	if _, err := svc.Refresh(context.Background(), "token-invalido"); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("esperaba ErrInvalidRefresh, got %v", err)
	}
}

func TestTokenHashDeterministic(t *testing.T) {
	if tokenHash("abc") != tokenHash("abc") {
		t.Fatal("tokenHash debe ser determinista")
	}
	if tokenHash("abc") == tokenHash("abd") {
		t.Fatal("tokenHash de tokens distintos no puede coincidir")
	}
}
