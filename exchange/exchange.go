package exchange

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/acoshift/go-services/wallet"
)

// Errors
var (
	ErrInvalidValue = errors.New("exchange: invalid order value")
	ErrInvalidSide  = errors.New("exchange: invalid side")
	ErrInvalidRate  = errors.New("exchange: invalid rate")
)

// Exchange is exchange service
type Exchange interface {
	// PlaceOrder places new order
	PlaceOrder(ctx context.Context, order Order) (orderID string, err error)

	// CancelOrder cancels a order
	CancelOrder(ctx context.Context, orderID string) error

	// Matching runs order matching algorithm
	Matching(ctx context.Context) error
}

// Repository is exchange storage
type Repository interface {
	CreateOrder(ctx context.Context, order *Order) (orderID string, err error)
	GetFee(ctx context.Context, userID string, side Side, rate, matched decimal.Decimal) (decimal.Decimal, error)
}

// Currency is exchange currency
type Currency struct {
	Bid   string
	Offer string
}

// New creates new exchange
func New(repo Repository, wallet wallet.Wallet, currency Currency) Exchange {
	return &service{repo, wallet, currency}
}

type service struct {
	repo     Repository
	wallet   wallet.Wallet
	currency Currency
}

func (s *service) PlaceOrder(ctx context.Context, order Order) (string, error) {
	if order.Value.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidValue
	}
	if order.Rate.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidRate
	}

	var currency string
	switch order.Side {
	case Bid:
		currency = s.currency.Bid
	case Offer:
		currency = s.currency.Offer
	default:
		return "", ErrInvalidSide
	}

	order.Remaining = order.Value

	err := s.wallet.Add(ctx, order.UserID, currency, order.Value.Neg())
	if err != nil {
		return "", err
	}

	return s.repo.CreateOrder(ctx, &order)
}

func (s *service) CancelOrder(ctx context.Context, orderID string) error {
	return nil
}

func (s *service) Matching(ctx context.Context) error {
	return nil
}
