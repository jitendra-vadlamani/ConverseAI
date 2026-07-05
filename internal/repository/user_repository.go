package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ai-chat/internal/model"
	"github.com/google/uuid"
)

type UserRepository interface {
	Create(ctx context.Context, email, passwordHash string) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	GetByID(ctx context.Context, id string) (*model.User, error)
	UpdatePassword(ctx context.Context, id string, passwordHash string) error
}

type PostgresUserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) Create(ctx context.Context, email, passwordHash string) (*model.User, error) {
	id := uuid.New().String()
	now := time.Now()
	
	query := `INSERT INTO users (id, email, password_hash, created_at) VALUES ($1, $2, $3, $4)`
	_, err := r.db.ExecContext(ctx, query, id, email, passwordHash, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create user in postgres: %w", err)
	}
	
	return &model.User{
		ID:        id,
		Email:     email,
		CreatedAt: now,
	}, nil
}

func (r *PostgresUserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	query := `SELECT id, email, password_hash, created_at FROM users WHERE email = $1`
	var u model.User
	err := r.db.QueryRowContext(ctx, query, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by email from postgres: %w", err)
	}
	return &u, nil
}

func (r *PostgresUserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	query := `SELECT id, email, password_hash, created_at FROM users WHERE id = $1`
	var u model.User
	err := r.db.QueryRowContext(ctx, query, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by ID from postgres: %w", err)
	}
	return &u, nil
}

func (r *PostgresUserRepository) UpdatePassword(ctx context.Context, id string, passwordHash string) error {
	query := `UPDATE users SET password_hash = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, passwordHash, id)
	if err != nil {
		return fmt.Errorf("failed to update user password in postgres: %w", err)
	}
	return nil
}
