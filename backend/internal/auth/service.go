package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/users"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("credenciales inválidas")
	ErrInvalidRefresh     = errors.New("token de refresco inválido")
)

type Service struct {
	cfg    *config.Config
	users  users.UserRepository
	tokens TokenStore
}

// TokenStore es el almacenamiento de tokens de refresco.
type TokenStore interface {
	Insert(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	Lookup(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, err error)
	Revoke(ctx context.Context, tokenHash string) error
}

type pgTokenStore struct{ pool *pgxpool.Pool }

func NewService(cfg *config.Config, store users.UserRepository, pool *pgxpool.Pool) *Service {
	return &Service{cfg: cfg, users: store, tokens: &pgTokenStore{pool: pool}}
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (s *Service) Register(ctx context.Context, email, password, fullName string) (*users.User, *Tokens, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, nil, err
	}
	u, err := s.users.Create(ctx, email, string(hash), fullName)
	if err != nil {
		return nil, nil, err
	}
	tokens, err := s.issueTokens(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	return u, tokens, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*users.User, *Tokens, error) {
	u, err := s.users.ByEmail(ctx, email)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, nil, ErrInvalidCredentials
	}
	tokens, err := s.issueTokens(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	return u, tokens, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	hash := tokenHash(refreshToken)
	userID, expiresAt, err := s.tokens.Lookup(ctx, hash)
	if err != nil {
		return nil, err
	}
	if userID == "" {
		return nil, ErrInvalidRefresh
	}
	if time.Now().After(expiresAt) {
		return nil, ErrInvalidRefresh
	}
	u, err := s.users.ByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.revokeToken(ctx, hash); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, u)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.revokeToken(ctx, tokenHash(refreshToken))
}

func (s *Service) issueTokens(ctx context.Context, u *users.User) (*Tokens, error) {
	now := time.Now()
	accessExp := now.Add(s.cfg.JWTAccessTTL)
	claims := jwt.RegisteredClaims{
		Subject:   u.ID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(accessExp),
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	refreshExp := now.Add(s.cfg.JWTRefreshTTL)
	if err := s.tokens.Insert(ctx, u.ID, tokenHash(refresh), refreshExp); err != nil {
		return nil, err
	}
	return &Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.cfg.JWTAccessTTL.Seconds()),
	}, nil
}

func (s *Service) revokeToken(ctx context.Context, hash string) error {
	return s.tokens.Revoke(ctx, hash)
}

func (s *pgTokenStore) Insert(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt)
	return err
}

func (s *pgTokenStore) Lookup(ctx context.Context, tokenHash string) (string, time.Time, error) {
	var userID string
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, expires_at FROM refresh_tokens WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash).Scan(&userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return userID, expiresAt, nil
}

func (s *pgTokenStore) Revoke(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1`, tokenHash)
	return err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}
