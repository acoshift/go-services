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
	GetActiveBuyOrderHighestRate(ctx context.Context) (Order, error)
	GetActiveSellOrderLowestRate(ctx context.Context) (Order, error)
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
	if !ValidSide(side) {
		return "", ErrInvalidSide
	}

	currency := s.getCurrency(ctx, side)
	err := s.wallet.Add(ctx, userID, currency, value.Neg())
	if err != nil {
		return "", err
	}

	orderID, err := s.repo.CreateOrder(ctx, Order{
		UserID:    userID,
		Side:      side,
		Rate:      rate,
		Value:     value,
		Remaining: value,
		Status:    Active,
	})
	if err != nil {
		return "", err
	}

	err = s.matchingOrder(ctx, orderID)
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
		Side:      side,
		Value:     value,
		Remaining: value,
		Status:    Active,
	})
	if err != nil {
		return "", err
	}

	err = s.matchingOrder(ctx, orderID)
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
	err = s.wallet.Add(ctx, order.UserID, currency, order.Remaining)
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

	switch order.Side {
	case Buy:
		err = s.matchingBuyOrder(ctx, &order)
	case Sell:
		err = s.matchingSellOrder(ctx, &order)
	default:
		return ErrInvalidSide
	}
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

func (s *service) matchingBuyOrder(ctx context.Context, order *Order) error {
	matchOrder, err := s.repo.GetActiveSellOrderLowestRate(ctx)
	if err == ErrOrderNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	if order.IsLimit() && matchOrder.Rate.GreaterThan(order.Rate) {
		// no more match order
		return nil
	}

	rate := matchOrder.Rate

	var buyAmount decimal.Decimal

	if m := matchOrder.Remaining.Mul(rate); order.Remaining.LessThanOrEqual(m) {
		buyAmount = order.Remaining
	} else {
		buyAmount = m
	}

	sellAmount := buyAmount.Div(rate)

	order.Remaining = order.Remaining.Sub(buyAmount)
	matchOrder.Remaining = matchOrder.Remaining.Sub(sellAmount)

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

	buyerFee, err := s.repo.GetFee(ctx, order.UserID, order.Side, order.Rate, sellAmount)
	if err != nil {
		return err
	}
	sellerFee, err := s.repo.GetFee(ctx, matchOrder.UserID, matchOrder.Side, matchOrder.Rate, buyAmount)
	if err != nil {
		return err
	}

	err = s.repo.InsertHistory(ctx, *order, matchOrder, Buy, rate, sellAmount, buyerFee, sellerFee)
	if err != nil {
		return err
	}

	err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, Sell), sellAmount.Sub(buyerFee))
	if err != nil {
		return err
	}
	err = s.wallet.Add(ctx, matchOrder.UserID, s.getCurrency(ctx, Buy), buyAmount.Sub(sellerFee))
	if err != nil {
		return err
	}

	if order.IsLimit() && !order.Rate.Equal(rate) {
		diffRate := order.Rate.Sub(rate)
		diffAmount := buyAmount.Mul(diffRate)

		if diffAmount.GreaterThan(decimal.Zero) {
			err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, Buy), diffAmount)
			if err != nil {
				return err
			}
		}
	}

	if order.Status == Active {
		return s.matchingBuyOrder(ctx, order)
	}

	return nil
}

func (s *service) matchingSellOrder(ctx context.Context, order *Order) error {
	matchOrder, err := s.repo.GetActiveBuyOrderHighestRate(ctx)
	if err == ErrOrderNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	if order.IsLimit() && matchOrder.Rate.LessThan(order.Rate) {
		// no more match order
		return nil
	}

	rate := matchOrder.Rate

	var sellAmount decimal.Decimal

	if m := matchOrder.Remaining.Div(rate); order.Remaining.LessThanOrEqual(m) {
		sellAmount = order.Remaining
	} else {
		sellAmount = m
	}

	buyAmount := sellAmount.Mul(rate)

	order.Remaining = order.Remaining.Sub(sellAmount)
	matchOrder.Remaining = matchOrder.Remaining.Sub(buyAmount)

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

	buyerFee, err := s.repo.GetFee(ctx, matchOrder.UserID, matchOrder.Side, matchOrder.Rate, sellAmount)
	if err != nil {
		return err
	}
	sellerFee, err := s.repo.GetFee(ctx, order.UserID, order.Side, order.Rate, buyAmount)
	if err != nil {
		return err
	}

	err = s.repo.InsertHistory(ctx, *order, matchOrder, Sell, rate, sellAmount, sellerFee, buyerFee)
	if err != nil {
		return err
	}

	err = s.wallet.Add(ctx, order.UserID, s.getCurrency(ctx, Buy), buyAmount.Sub(sellerFee))
	if err != nil {
		return err
	}
	err = s.wallet.Add(ctx, matchOrder.UserID, s.getCurrency(ctx, Sell), sellAmount.Sub(buyerFee))
	if err != nil {
		return err
	}

	if order.Status == Active {
		return s.matchingSellOrder(ctx, order)
	}

	return nil
}
