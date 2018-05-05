package wallet

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
)

// Errors
var (
	ErrBalanceNotEnough = errors.New("wallet: balance is not enough")
	ErrInvalidValue     = errors.New("wallet: invalid value")
)

// Wallet is wallet service
type Wallet interface {
	// Balance gets user's balance
	Balance(ctx context.Context, userID string, currency string) (decimal.Decimal, error)

	// Add adds fund to a wallet
	Add(ctx context.Context, userID string, currency string, value decimal.Decimal) error

	// Transfer transfers fund from src to dst wallet
	Transfer(ctx context.Context, srcUserID string, dstUserID string, currency string, value decimal.Decimal) error
}

// Repository is wallet storage
type Repository interface {
	AddBalance(ctx context.Context, userID string, currency string, value decimal.Decimal) error
	GetBalance(ctx context.Context, userID string, currency string) (decimal.Decimal, error)

	InsertTx(ctx context.Context, userID string, currency string, value decimal.Decimal) error
}

// New creates new wallet service
func New(repo Repository) Wallet {
	return &service{repo}
}

type service struct {
	repo Repository
}

func (s *service) Balance(ctx context.Context, userID string, currency string) (decimal.Decimal, error) {
	return s.repo.GetBalance(ctx, userID, currency)
}

func (s *service) Add(ctx context.Context, userID string, currency string, value decimal.Decimal) error {
	if value.Equal(decimal.Zero) {
		// short-circuit for empty value
		return nil
	}

	// can not debt more than current balance
	if value.LessThan(decimal.Zero) {
		b, err := s.repo.GetBalance(ctx, userID, currency)
		if err != nil {
			return err
		}
		if b.LessThan(value) {
			return ErrBalanceNotEnough
		}
	}

	err := s.repo.AddBalance(ctx, userID, currency, value)
	if err != nil {
		return err
	}

	err = s.repo.InsertTx(ctx, userID, currency, value)
	if err != nil {
		return err
	}

	return nil
}

func (s *service) Transfer(ctx context.Context, srcUserID string, dstUserID string, currency string, value decimal.Decimal) error {
	if value.Equal(decimal.Zero) {
		// short-circuit for empty value
		return nil
	}

	if value.LessThan(decimal.Zero) {
		return ErrInvalidValue
	}

	err := s.Add(ctx, srcUserID, currency, value.Neg())
	if err != nil {
		return err
	}

	err = s.Add(ctx, dstUserID, currency, value)
	if err != nil {
		return err
	}

	return nil
}
