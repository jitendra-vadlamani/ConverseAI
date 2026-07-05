package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-chat/internal/config"
	"ai-chat/internal/model"
	"ai-chat/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService interface {
	Register(ctx context.Context, email, password string) (*model.User, error)
	Login(ctx context.Context, email, password string) (*model.User, string, error)
	VerifyToken(tokenString string) (string, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	UpdatePassword(ctx context.Context, userID, oldPassword, newPassword string) error
}

type authService struct {
	repo      repository.UserRepository
	jwtSecret []byte
}

func NewAuthService(repo repository.UserRepository, cfg *config.Config) AuthService {
	return &authService{
		repo:      repo,
		jwtSecret: []byte(cfg.JWTSecret),
	}
}

func (s *authService) Register(ctx context.Context, email, password string) (*model.User, error) {
	// Input validation
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return nil, errors.New("invalid email address")
	}
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}

	// Check if user already exists
	existingUser, _ := s.repo.GetByEmail(ctx, email)
	if existingUser != nil {
		return nil, errors.New("user already exists")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	return s.repo.Create(ctx, email, string(hashedPassword))
}

func (s *authService) Login(ctx context.Context, email, password string) (*model.User, string, error) {
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil || user == nil {
		return nil, "", errors.New("invalid email or password")
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", errors.New("invalid email or password")
	}

	// Generate JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": user.ID,
		"exp": time.Now().Add(time.Hour * 24).Unix(),
	})

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate token: %w", err)
	}

	return user, tokenString, nil
}

func (s *authService) VerifyToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	id, ok := claims["sub"].(string)
	if !ok {
		return "", errors.New("invalid token sub claim")
	}

	return id, nil
}

func (s *authService) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *authService) UpdatePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("invalid current password")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update in repo
	return s.repo.UpdatePassword(ctx, userID, string(hashedPassword))
}
