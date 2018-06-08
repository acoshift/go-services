package exchange

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/acoshift/go-services/wallet"
)

// Errors
var (
	ErrInvalidValue  = errors.New("exchange: invalid order value")
	ErrInvalidSide   = errors.New("exchange: invalid side")
	ErrInvalidRate   = errors.New("exchange: invalid rate")
	ErrOrderNotFound = errors.New("exchange: order not found")
)

// Exchange is exchange service
type Exchange interface {
	// PlaceOrder places new order
	PlaceOrder(ctx context.Context, order Order) (orderID string, err error)

	// CancelOrder cancels a order
	CancelOrder(ctx context.Context, orderID string) error
}

// Repository is exchange storage
type Repository interface {
	CreateOrder(ctx context.Context, order Order) (orderID string, err error)
	GetOrder(ctx context.Context, orderID string) (Order, error)
	SetOrderStatus(ctx context.Context, orderID string, status Status) error
	SetOrderStatusRemainingAndStampMatched(ctx context.Context, orderID string, status Status, remaining decimal.Decimal) error
	StampOrderFinished(ctx context.Context, orderID string) error
	GetFee(ctx context.Context, userID string, side Side, rate, amount decimal.Decimal) (decimal.Decimal, error)
	GetOrderHighestRate(ctx context.Context, side Side, status Status, minRate decimal.Decimal) (Order, error)
	GetOrderLowestRate(ctx context.Context, side Side, status Status, maxRate decimal.Decimal) (Order, error)
	InsertHistory(ctx context.Context, srcOrder, dstOrder Order, side Side, rate, amount decimal.Decimal) error
}

// CurrencyGetter is the function that return currency
type CurrencyGetter func(context.Context) string

// Currency is exchange currency
type Currency struct {
	Buy  CurrencyGetter
	Sell CurrencyGetter
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

func (s *service) getCurrency(ctx context.Context, side Side) string {
	switch side {
	case Buy:
		return s.currency.Buy(ctx)
	case Sell:
		return s.currency.Sell(ctx)
	default:
		panic("unreachable")
	}
}

func (s *service) swapSide(side Side) Side {
	switch side {
	case Buy:
		return Sell
	case Sell:
		return Buy
	default:
		panic("unreachable")
	}
}

func (s *service) PlaceOrder(ctx context.Context, order Order) (string, error) {
	if order.Value.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidValue
	}
	if order.Rate.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidRate
	}
	if order.Side != Buy && order.Side != Sell {
		return "", ErrInvalidSide
	}

	order.Status = Active
	order.Remaining = order.Value

	currency := s.getCurrency(ctx, order.Side)
	amount := order.Value.Mul(order.Rate)
	err := s.wallet.Add(ctx, order.UserID, currency, amount.Neg())
	if err != nil {
		return "", err
	}

	orderID, err := s.repo.CreateOrder(ctx, order)
	if err != nil {
		return "", err
	}

	err = s.matchingOrder(ctx, orderID)
	if err != nil {
		return "", err
	}

	return orderID, nil
}

func (s *service) CancelOrder(ctx context.Context, orderID string) error {
	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.repo.SetOrderStatus(ctx, order.ID, Cancelled)
	if err != nil {
		return err
	}

	err = s.repo.StampOrderFinished(ctx, order.ID)
	if err != nil {
		return err
	}

	currency := s.getCurrency(ctx, order.Side)
	amount := order.Remaining.Mul(order.Rate)
	err = s.wallet.Add(ctx, order.UserID, currency, amount)
	if err != nil {
		return err
	}

	return nil
}

func (s *service) matchingOrder(ctx context.Context, orderID string) error {
	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}

	if order.Status != Active {
		return nil
	}

	if order.Remaining.LessThanOrEqual(decimal.Zero) {
		return nil
	}

	var matchOrder Order
	switch order.Side {
	case Buy:
		matchOrder, err = s.repo.GetOrderLowestRate(ctx, Sell, Active, order.Rate)
	case Sell:
		matchOrder, err = s.repo.GetOrderHighestRate(ctx, Buy, Active, order.Rate)
	}
	if err == ErrOrderNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	var amount decimal.Decimal

	if order.Remaining.LessThanOrEqual(matchOrder.Remaining) {
		amount = order.Remaining
	} else {
		amount = matchOrder.Remaining
	}

	order.Remaining = order.Remaining.Sub(amount)
	matchOrder.Remaining = matchOrder.Remaining.Sub(amount)

	if order.Remaining.LessThanOrEqual(decimal.Zero) {
		order.Status = Matched

		err = s.repo.StampOrderFinished(ctx, order.ID)
		if err != nil {
			return err
		}
	}

	if matchOrder.Remaining.LessThanOrEqual(decimal.Zero) {
		matchOrder.Status = Matched

		err = s.repo.StampOrderFinished(ctx, matchOrder.ID)
		if err != nil {
			return err
		}
	}

	err = s.repo.SetOrderStatusRemainingAndStampMatched(ctx, order.ID, order.Status, order.Remaining)
	if err != nil {
		return err
	}
	err = s.repo.SetOrderStatusRemainingAndStampMatched(ctx, matchOrder.ID, matchOrder.Status, matchOrder.Remaining)
	if err != nil {
		return err
	}

	err = s.repo.InsertHistory(ctx, order, matchOrder, order.Side, matchOrder.Rate, amount)
	if err != nil {
		return err
	}

	err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, matchOrder.Side), amount)
	if err != nil {
		return err
	}
	err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, order.Side), amount.Mul(matchOrder.Rate))
	if err != nil {
		return err
	}

	if !order.Rate.Equal(matchOrder.Rate) {
		diffRate := order.Rate.Sub(matchOrder.Rate)
		diffAmount := amount.Mul(diffRate)

		if diffAmount.GreaterThan(decimal.Zero) {
			err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, matchOrder.Side), diffAmount)
			if err != nil {
				return err
			}
		}
	}

	if order.Remaining.GreaterThan(decimal.Zero) {
		return s.matchingOrder(ctx, order.ID)
	}

	return nil
}
