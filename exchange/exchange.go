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
	ErrInvalidSide   = errors.New("exchange: invalid order side")
	ErrInvalidRate   = errors.New("exchange: invalid order rate")
	ErrInvalidType   = errors.New("exchange: invalid order type")
	ErrOrderNotFound = errors.New("exchange: order not found")
)

// Exchange is exchange service
type Exchange interface {
	// PlaceLimitOrder places a limit order
	PlaceLimitOrder(ctx context.Context, userID string, side Side, rate, value decimal.Decimal) (orderID string, err error)

	// PlaceMarketOrder places a market order
	PlaceMarketOrder(ctx context.Context, userID string, side Side, value decimal.Decimal) (orderID string, err error)

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
	GetActiveBuyLimitOrderHighestRate(ctx context.Context) (Order, error)
	GetActiveSellLimitOrderLowestRate(ctx context.Context) (Order, error)
	InsertHistory(ctx context.Context, srcOrder, dstOrder Order, side Side, rate, amount, srcFee, dstFee decimal.Decimal) error
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

func (s *service) PlaceLimitOrder(ctx context.Context, userID string, side Side, rate, value decimal.Decimal) (string, error) {
	if value.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidValue
	}
	if rate.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidRate
	}

	var err error
	switch side {
	case Buy:
		err = s.wallet.Add(ctx, userID, s.getCurrency(ctx, side), value.Mul(rate).Neg())
	case Sell:
		err = s.wallet.Add(ctx, userID, s.getCurrency(ctx, side), value.Neg())
	default:
		return "", ErrInvalidSide
	}
	if err != nil {
		return "", err
	}

	orderID, err := s.repo.CreateOrder(ctx, Order{
		UserID:    userID,
		Type:      Limit,
		Side:      side,
		Rate:      rate,
		Value:     value,
		Remaining: value,
		Status:    Active,
	})
	if err != nil {
		return "", err
	}

	err = s.matchingLimitOrder(ctx, orderID)
	if err != nil {
		return "", err
	}

	return orderID, nil
}

func (s *service) PlaceMarketOrder(ctx context.Context, userID string, side Side, value decimal.Decimal) (string, error) {
	if value.LessThanOrEqual(decimal.Zero) {
		return "", ErrInvalidValue
	}
	if !ValidSide(side) {
		return "", ErrInvalidSide
	}

	orderID, err := s.repo.CreateOrder(ctx, Order{
		UserID:    userID,
		Type:      Market,
		Side:      side,
		Value:     value,
		Remaining: value,
		Status:    Active,
	})
	if err != nil {
		return "", err
	}

	err = s.matchingMarketOrder(ctx, orderID)
	if err != nil {
		return "", err
	}

	err = s.CancelOrder(ctx, orderID)
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

	if order.Status != Active {
		return nil
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
	switch order.Side {
	case Buy:
		err = s.wallet.Add(ctx, order.UserID, currency, order.Remaining.Mul(order.Rate))
	case Sell:
		err = s.wallet.Add(ctx, order.UserID, currency, order.Remaining)
	default:
		return ErrInvalidSide
	}
	if err != nil {
		return err
	}

	return nil
}

func (s *service) matchingLimitOrder(ctx context.Context, orderID string) error {
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

	err = s.runLimitMatching(ctx, &order)
	if err != nil {
		return err
	}

	err = s.repo.SetOrderStatusRemainingAndStampMatched(ctx, order.ID, order.Status, order.Remaining)
	if err != nil {
		return err
	}

	if order.Status == Matched {
		err = s.repo.StampOrderFinished(ctx, order.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *service) runLimitMatching(ctx context.Context, order *Order) error {
	var matchOrder Order
	var err error

	switch order.Side {
	case Buy:
		matchOrder, err = s.repo.GetActiveSellLimitOrderLowestRate(ctx)
		if err == ErrOrderNotFound {
			return nil
		}
		if err != nil {
			return err
		}

		if matchOrder.Rate.GreaterThan(order.Rate) {
			// no more match order
			return nil
		}
	case Sell:
		matchOrder, err = s.repo.GetActiveBuyLimitOrderHighestRate(ctx)
		if err == ErrOrderNotFound {
			return nil
		}
		if err != nil {
			return err
		}

		if matchOrder.Rate.LessThan(order.Rate) {
			// no more match order
			return nil
		}
	default:
		return ErrInvalidSide
	}

	rate := matchOrder.Rate

	var amount decimal.Decimal
	if matchOrder.Remaining.LessThanOrEqual(order.Remaining) {
		amount = matchOrder.Remaining
	} else {
		amount = order.Remaining
	}

	order.Remaining = order.Remaining.Sub(amount)
	matchOrder.Remaining = matchOrder.Remaining.Sub(amount)

	if order.Remaining.LessThanOrEqual(decimal.Zero) {
		order.Status = Matched
	}

	if matchOrder.Remaining.LessThanOrEqual(decimal.Zero) {
		matchOrder.Status = Matched

		err = s.repo.StampOrderFinished(ctx, matchOrder.ID)
		if err != nil {
			return err
		}
	}

	err = s.repo.SetOrderStatusRemainingAndStampMatched(ctx, matchOrder.ID, matchOrder.Status, matchOrder.Remaining)
	if err != nil {
		return err
	}

	orderFee, err := s.repo.GetFee(ctx, order.UserID, order.Side, order.Rate, amount)
	if err != nil {
		return err
	}
	matchOrderFee, err := s.repo.GetFee(ctx, matchOrder.UserID, matchOrder.Side, matchOrder.Rate, amount)
	if err != nil {
		return err
	}

	err = s.repo.InsertHistory(ctx, *order, matchOrder, Buy, rate, amount, orderFee, matchOrderFee)
	if err != nil {
		return err
	}

	if order.Side == Buy {
		err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, matchOrder.Side), amount.Sub(orderFee))
		if err != nil {
			return err
		}
		err = s.wallet.Add(ctx, matchOrder.UserID, s.getCurrency(ctx, order.Side), amount.Sub(matchOrderFee).Mul(rate))
		if err != nil {
			return err
		}
	} else {
		err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, matchOrder.Side), amount.Sub(orderFee).Mul(rate))
		if err != nil {
			return err
		}
		err = s.wallet.Add(ctx, matchOrder.UserID, s.getCurrency(ctx, order.Side), amount.Sub(matchOrderFee))
		if err != nil {
			return err
		}
	}

	if order.Side == Buy && !order.Rate.Equal(rate) {
		diffRate := order.Rate.Sub(rate)
		diffAmount := amount.Mul(diffRate)

		if diffAmount.GreaterThan(decimal.Zero) {
			err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, order.Side), diffAmount)
			if err != nil {
				return err
			}
		}
	}

	if order.Status == Active {
		return s.runLimitMatching(ctx, order)
	}

	return nil
}

func (s *service) matchingMarketOrder(ctx context.Context, orderID string) error {
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

	err = s.runMarketMatching(ctx, &order)
	if err != nil {
		return err
	}

	err = s.repo.SetOrderStatusRemainingAndStampMatched(ctx, order.ID, order.Status, order.Remaining)
	if err != nil {
		return err
	}

	if order.Status == Matched {
		err = s.repo.StampOrderFinished(ctx, order.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *service) runMarketMatching(ctx context.Context, order *Order) error {
	var matchOrder Order
	var err error

	switch order.Side {
	case Buy:
		matchOrder, err = s.repo.GetActiveSellLimitOrderLowestRate(ctx)
	case Sell:
		matchOrder, err = s.repo.GetActiveBuyLimitOrderHighestRate(ctx)
	default:
		return ErrInvalidSide
	}
	if err == ErrOrderNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	rate := matchOrder.Rate

	var amount decimal.Decimal
	if matchOrder.Remaining.LessThanOrEqual(order.Remaining) {
		amount = matchOrder.Remaining
	} else {
		amount = order.Remaining
	}

	order.Remaining = order.Remaining.Sub(amount)
	matchOrder.Remaining = matchOrder.Remaining.Sub(amount)

	if order.Remaining.LessThanOrEqual(decimal.Zero) {
		order.Status = Matched
	}

	if matchOrder.Remaining.LessThanOrEqual(decimal.Zero) {
		matchOrder.Status = Matched

		err = s.repo.StampOrderFinished(ctx, matchOrder.ID)
		if err != nil {
			return err
		}
	}

	err = s.repo.SetOrderStatusRemainingAndStampMatched(ctx, matchOrder.ID, matchOrder.Status, matchOrder.Remaining)
	if err != nil {
		return err
	}

	orderFee, err := s.repo.GetFee(ctx, order.UserID, order.Side, order.Rate, amount)
	if err != nil {
		return err
	}
	matchOrderFee, err := s.repo.GetFee(ctx, matchOrder.UserID, matchOrder.Side, matchOrder.Rate, amount)
	if err != nil {
		return err
	}

	err = s.repo.InsertHistory(ctx, *order, matchOrder, Buy, rate, amount, orderFee, matchOrderFee)
	if err != nil {
		return err
	}

	err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, matchOrder.Side), amount.Sub(orderFee))
	if err != nil {
		return err
	}
	err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, order.Side), amount.Mul(rate).Neg())
	if err != nil {
		return err
	}
	err = s.wallet.Add(ctx, matchOrder.UserID, s.getCurrency(ctx, order.Side), amount.Sub(matchOrderFee).Mul(rate))
	if err != nil {
		return err
	}

	if order.Status == Active {
		return s.runMarketMatching(ctx, order)
	}

	return nil
}
