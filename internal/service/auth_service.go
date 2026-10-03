package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/repository"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt ignores everything past 72 bytes
)

// Errors with a user-facing meaning; handlers map them to status codes.
var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailTaken         = errors.New("an account with this email already exists")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotFound           = errors.New("not found")
	ErrConflict           = errors.New("conflict")
)

// ValidationError carries a message that is safe to show to the user.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }
func (e *ValidationError) Unwrap() error { return ErrInvalidInput }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

type AuthService interface {
	Register(ctx context.Context, email, password string) (*model.User, error)
	Login(ctx context.Context, email, password string) (*model.User, string, error)
	VerifyToken(tokenString string) (string, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	UpdatePassword(ctx context.Context, userID, oldPassword, newPassword string) error
	// CheckPassword verifies the current password (for destructive actions).
	CheckPassword(ctx context.Context, userID, password string) error
}

type authService struct {
	repo      repository.UserRepository
	jwtSecret []byte
	ttl       time.Duration
	dummyHash []byte
}

func NewAuthService(repo repository.UserRepository, jwtSecret string, ttl time.Duration) AuthService {
	// Comparing against a dummy hash when the user doesn't exist keeps login
	// timing the same for known and unknown emails.
	dummy, _ := bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcrypt.DefaultCost)
	return &authService{repo: repo, jwtSecret: []byte(jwtSecret), ttl: ttl, dummyHash: dummy}
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validatePassword(pw string) error {
	if len(pw) < minPasswordLen {
		return invalid("password must be at least %d characters", minPasswordLen)
	}
	if len(pw) > maxPasswordLen {
		return invalid("password must be at most %d bytes", maxPasswordLen)
	}
	return nil
}

func (s *authService) Register(ctx context.Context, email, password string) (*model.User, error) {
	email = NormalizeEmail(email)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") || len(email) > 254 {
		return nil, invalid("invalid email address")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.repo.Create(ctx, email, string(hash))
	if errors.Is(err, repository.ErrConflict) {
		return nil, ErrEmailTaken
	}
	return user, err
}

func (s *authService) Login(ctx context.Context, email, password string) (*model.User, string, error) {
	user, err := s.repo.GetByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, repository.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, "", ErrInvalidCredentials
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": user.ID.Hex(),
		"iat": now.Unix(),
		"exp": now.Add(s.ttl).Unix(),
	})
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, "", fmt.Errorf("sign token: %w", err)
	}
	return user, signed, nil
}

func (s *authService) VerifyToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(*jwt.Token) (any, error) { return s.jwtSecret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}
	sub, err := token.Claims.GetSubject()
	if err != nil {
		return "", errors.New("invalid token subject")
	}
	if _, err := primitive.ObjectIDFromHex(sub); err != nil {
		return "", errors.New("invalid token subject")
	}
	return sub, nil
}

func (s *authService) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrNotFound
	}
	user, err := s.repo.GetByID(ctx, objID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return user, err
}

func (s *authService) CheckPassword(ctx context.Context, userID, password string) error {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return ErrInvalidCredentials
	}
	return nil
}

func (s *authService) UpdatePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	if err := s.CheckPassword(ctx, userID, oldPassword); err != nil {
		return err
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	objID, _ := primitive.ObjectIDFromHex(userID)
	return s.repo.UpdatePassword(ctx, objID, string(hash))
}
