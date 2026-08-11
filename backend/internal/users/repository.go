package users

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("usuario no encontrado")
	ErrEmailTaken = errors.New("el email ya está registrado")
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	FullName     string
	CreatedAt    time.Time
}

// UserRepository es el contrato que consume el servicio de auth.
type UserRepository interface {
	Create(ctx context.Context, email, passwordHash, fullName string) (*User, error)
	ByEmail(ctx context.Context, email string) (*User, error)
	ByID(ctx context.Context, id string) (*User, error)
}

type PostgresUserRepository struct{ pool *pgxpool.Pool }

func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

func (r *PostgresUserRepository) Create(ctx context.Context, email, passwordHash, fullName string) (*User, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, full_name) VALUES ($1, $2, $3, $4)`,
		id, email, passwordHash, fullName)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, FullName: fullName}, nil
}

func (r *PostgresUserRepository) ByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, full_name, created_at FROM users WHERE email = $1`,
		email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *PostgresUserRepository) ByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, full_name, created_at FROM users WHERE id = $1`,
		id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}
