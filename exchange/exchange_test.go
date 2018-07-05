package exchange_test

import (
	"context"
	"testing"
	"time"

	"github.com/satori/go.uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/acoshift/go-services/exchange"
	"github.com/acoshift/go-services/wallet"
)

func genID() string {
	return uuid.Must(uuid.NewV4()).String()
}

type memoryExchangeRepository struct {
	data []exchange.Order
}

func (r *memoryExchangeRepository) CreateOrder(ctx context.Context, order exchange.Order) (orderID string, err error) {
	order.ID = genID()
	order.CreatedAt = time.Now()
	r.data = append(r.data, order)
	return order.ID, nil
}

func (r *memoryExchangeRepository) GetOrder(ctx context.Context, orderID string) (exchange.Order, error) {
	for _, order := range r.data {
		if order.ID == orderID {
			return order, nil
		}
	}
	return exchange.Order{}, exchange.ErrOrderNotFound
}

func (r *memoryExchangeRepository) SetOrderStatus(ctx context.Context, orderID string, status exchange.Status) error {
	for i, order := range r.data {
		if order.ID == orderID {
			r.data[i].Status = status
			return nil
		}
	}
	return nil
}

func (r *memoryExchangeRepository) SetOrderStatusRemainingAndStampMatched(ctx context.Context, orderID string, status exchange.Status, remaining decimal.Decimal) error {
	for i, order := range r.data {
		if order.ID == orderID {
			r.data[i].Status = status
			r.data[i].Remaining = remaining
			r.data[i].MatchedAt = time.Now()
			return nil
		}
	}
	return nil
}

func (r *memoryExchangeRepository) StampOrderFinished(ctx context.Context, orderID string) error {
	for i, order := range r.data {
		if order.ID == orderID {
			r.data[i].FinishedAt = time.Now()
			return nil
		}
	}
	return nil
}

func (r *memoryExchangeRepository) GetFee(ctx context.Context, userID string, side exchange.Side, rate, amount decimal.Decimal) (decimal.Decimal, error) {
	return amount.Mul(d("0.0025")), nil
}

func (r *memoryExchangeRepository) GetActiveBuyLimitOrderHighestRate(ctx context.Context) (result exchange.Order, err error) {
	for _, order := range r.data {
		if order.Side == exchange.Buy && order.Status == exchange.Active && order.Type == exchange.Limit {
			if result.Rate.Equal(decimal.Zero) {
				result = order
			} else if order.Rate.Equal(result.Rate) {
				if order.CreatedAt.Before(result.CreatedAt) {
					result = order
				}
			} else if order.Rate.GreaterThan(result.Rate) {
				result = order
			}
		}
	}
	if result.CreatedAt.IsZero() {
		err = exchange.ErrOrderNotFound
	}
	return
}

func (r *memoryExchangeRepository) GetActiveSellLimitOrderLowestRate(ctx context.Context) (result exchange.Order, err error) {
	for _, order := range r.data {
		if order.Side == exchange.Sell && order.Status == exchange.Active && order.Type == exchange.Limit {
			if result.Rate.Equal(decimal.Zero) {
				result = order
			} else if order.Rate.Equal(result.Rate) {
				if order.CreatedAt.Before(result.CreatedAt) {
					result = order
				}
			} else if order.Rate.LessThan(result.Rate) {
				result = order
			}
		}
	}
	if result.CreatedAt.IsZero() {
		err = exchange.ErrOrderNotFound
	}
	return
}

func (r *memoryExchangeRepository) InsertHistory(ctx context.Context, srcOrder, dstOrder exchange.Order, side exchange.Side, rate, amount, srcFee, dstFee decimal.Decimal) error {
	return nil
}

type memoryWalletRepository struct {
	// userID => currency => value
	data map[string]map[string]decimal.Decimal
}

func (r *memoryWalletRepository) ensureData(userID string) {
	if r.data == nil {
		r.data = make(map[string]map[string]decimal.Decimal)
	}
	if r.data[userID] == nil {
		r.data[userID] = make(map[string]decimal.Decimal)
	}
}

func (r *memoryWalletRepository) AddBalance(ctx context.Context, userID string, currency string, value decimal.Decimal) error {
	r.ensureData(userID)
	r.data[userID][currency] = r.data[userID][currency].Add(value)
	return nil
}

func (r *memoryWalletRepository) GetBalance(ctx context.Context, userID string, currency string) (decimal.Decimal, error) {
	r.ensureData(userID)
	return r.data[userID][currency], nil
}

func (r *memoryWalletRepository) InsertTx(ctx context.Context, userID string, currency string, value decimal.Decimal) error {
	return nil
}

var currency = exchange.Currency{
	Buy: func(context.Context) string {
		return "A"
	},
	Sell: func(context.Context) string {
		return "B"
	},
}

var ctx = context.Background()

func d(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

func add(t *testing.T, w wallet.Wallet, userID string, currency string, amount string) {
	w.Add(ctx, userID, currency, d(amount))
}

func bal(t *testing.T, w wallet.Wallet, userID string, currency string, equal string) {
	t.Helper()

	b, _ := w.Balance(ctx, userID, currency)
	assert.Equal(t, d(equal).String(), b.String())
}

func remain(t *testing.T, r exchange.Repository, orderID string, equal string) {
	t.Helper()

	order, _ := r.GetOrder(ctx, orderID)
	assert.Equal(t, d(equal).String(), order.Remaining.String())
}

func status(t *testing.T, r exchange.Repository, orderID string, s exchange.Status) {
	t.Helper()

	order, _ := r.GetOrder(ctx, orderID)
	assert.Equal(t, s, order.Status)
}

func placeLimit(t *testing.T, s exchange.Exchange, userID string, side exchange.Side, rate, amount string) string {
	t.Helper()

	orderID, err := s.PlaceLimitOrder(ctx, userID, side, d(rate), d(amount))
	assert.NoError(t, err)
	assert.NotEmpty(t, orderID)
	return orderID
}

func cancel(t *testing.T, s exchange.Exchange, orderID string) {
	t.Helper()

	err := s.CancelOrder(ctx, orderID)
	assert.NoError(t, err)
}

func TestExchangeBuy1(t *testing.T) {
	t.Parallel()

	r := new(memoryExchangeRepository)
	w := wallet.New(new(memoryWalletRepository))
	s := exchange.New(r, w, currency)

	add(t, w, "1", "A", "10000")
	add(t, w, "2", "B", "10000")

	order1 := placeLimit(t, s, "2", exchange.Sell, "2", "50")

	bal(t, w, "2", "A", "0")
	bal(t, w, "2", "B", "9950")

	order2 := placeLimit(t, s, "1", exchange.Buy, "2", "50")

	remain(t, r, order1, "0")
	status(t, r, order1, exchange.Matched)

	remain(t, r, order2, "0")
	status(t, r, order2, exchange.Matched)

	bal(t, w, "1", "A", "9900")
	bal(t, w, "1", "B", "49.875")
	bal(t, w, "2", "A", "99.75")
	bal(t, w, "2", "B", "9950")
}

func TestExchangeBuy2(t *testing.T) {
	t.Parallel()

	r := new(memoryExchangeRepository)
	w := wallet.New(new(memoryWalletRepository))
	s := exchange.New(r, w, currency)

	add(t, w, "1", "A", "10000")
	add(t, w, "2", "B", "10000")

	order1 := placeLimit(t, s, "2", exchange.Sell, "2", "100")

	bal(t, w, "2", "A", "0")
	bal(t, w, "2", "B", "9900")

	order2 := placeLimit(t, s, "1", exchange.Buy, "2", "60")

	remain(t, r, order1, "40")
	status(t, r, order1, exchange.Active)

	remain(t, r, order2, "0")
	status(t, r, order2, exchange.Matched)

	bal(t, w, "1", "A", "9880")
	bal(t, w, "1", "B", "59.85")
	bal(t, w, "2", "A", "119.7")
	bal(t, w, "2", "B", "9900")

	order3 := placeLimit(t, s, "1", exchange.Buy, "2", "60")

	remain(t, r, order1, "0")
	status(t, r, order1, exchange.Matched)

	remain(t, r, order2, "0")
	status(t, r, order2, exchange.Matched)

	remain(t, r, order3, "20")
	status(t, r, order3, exchange.Active)

	bal(t, w, "1", "A", "9760")
	bal(t, w, "1", "B", "99.75")
	bal(t, w, "2", "A", "199.5")
	bal(t, w, "2", "B", "9900")

	cancel(t, s, order3)
	remain(t, r, order3, "20")
	status(t, r, order3, exchange.Cancelled)

	bal(t, w, "1", "A", "9800")
	bal(t, w, "1", "B", "99.75")
	bal(t, w, "2", "A", "199.5")
	bal(t, w, "2", "B", "9900")

	cancel(t, s, order2)
	remain(t, r, order2, "0")
	status(t, r, order2, exchange.Matched)

	bal(t, w, "1", "A", "9800")
	bal(t, w, "1", "B", "99.75")
	bal(t, w, "2", "A", "199.5")
	bal(t, w, "2", "B", "9900")
}

func TestExchangeSell1(t *testing.T) {
	t.Parallel()

	r := new(memoryExchangeRepository)
	w := wallet.New(new(memoryWalletRepository))
	s := exchange.New(r, w, currency)

	add(t, w, "1", "A", "10000")
	add(t, w, "2", "B", "10000")

	order1 := placeLimit(t, s, "1", exchange.Buy, "2", "50")

	bal(t, w, "1", "A", "9900")
	bal(t, w, "1", "B", "0")

	order2 := placeLimit(t, s, "2", exchange.Sell, "2", "50")

	remain(t, r, order1, "0")
	status(t, r, order1, exchange.Matched)

	remain(t, r, order2, "0")
	status(t, r, order2, exchange.Matched)

	bal(t, w, "1", "A", "9900")
	bal(t, w, "1", "B", "49.875")
	bal(t, w, "2", "A", "99.75")
	bal(t, w, "2", "B", "9950")
}
