package totpuser

import (
	"context"
	"errors"

	"github.com/acoshift/go-services/totp"
)

// Errors
var (
	ErrInvalidPassword = errors.New("totpuser: invalid password")
	ErrAlreadyEnabled  = errors.New("totpuser: already enabled")
)

// TOTPUser is the TOTP service with user management
type TOTPUser interface {
	totp.TOTP

	// VerifyUser verifies user's password
	VerifyUser(ctx context.Context, userID string, password string) error

	// Enable enables user otp
	Enable(ctx context.Context, userID string, secret string, password string) error

	// Disable disables user otp
	Disable(ctx context.Context, userID string, password string) error

	// Set sets user otp secret without verify password
	Set(ctx context.Context, userID string, secret string) error

	// Remove removes user otp without verify password
	Remove(ctx context.Context, userID string) error

	// IsEnabled checks is user enable otp
	IsEnabled(ctx context.Context, userID string) (bool, error)
}

// Repository is the storage for TOTPUser
type Repository interface {
	SetUserOTPSecret(ctx context.Context, userID string, secret string) error
	GetUserOTPSecret(ctx context.Context, userID string) (secret string, err error)
}

// New creates new TOTPUser service
func New(totpService totp.TOTP, repo Repository) TOTPUser {
	return &service{totpService, repo}
}

type service struct {
	totp.TOTP
	repo Repository
}

func (s *service) VerifyUser(ctx context.Context, userID string, password string) error {
	secret, err := s.repo.GetUserOTPSecret(ctx, userID)
	if err != nil {
		return err
	}

	if secret == "" {
		return nil
	}

	if !s.Verify(secret, password) {
		return ErrInvalidPassword
	}

	return nil
}

func (s *service) Enable(ctx context.Context, userID string, secret string, password string) error {
	enabled, err := s.IsEnabled(ctx, userID)
	if err != nil {
		return err
	}
	if enabled {
		return ErrAlreadyEnabled
	}

	if !s.Verify(secret, password) {
		return ErrInvalidPassword
	}

	return s.repo.SetUserOTPSecret(ctx, userID, secret)
}

func (s *service) Disable(ctx context.Context, userID string, password string) error {
	err := s.VerifyUser(ctx, userID, password)
	if err != nil {
		return err
	}

	return s.repo.SetUserOTPSecret(ctx, userID, "")
}

func (s *service) Set(ctx context.Context, userID string, secret string) error {
	return s.repo.SetUserOTPSecret(ctx, userID, secret)
}

func (s *service) Remove(ctx context.Context, userID string) error {
	return s.repo.SetUserOTPSecret(ctx, userID, "")
}

func (s *service) IsEnabled(ctx context.Context, userID string) (bool, error) {
	secret, err := s.repo.GetUserOTPSecret(ctx, userID)
	if err != nil {
		return false, err
	}
	return secret != "", nil
}
