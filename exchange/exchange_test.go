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

func TestExchangeBuy1(t *testing.T) {
	repo := new(memoryExchangeRepository)
	walletRepo := new(memoryWalletRepository)
	w := wallet.New(walletRepo)
	svc := exchange.New(repo, w, currency)

	w.Add(ctx, "1", "A", d("10000"))
	w.Add(ctx, "2", "B", d("10000"))

	orderID1, err := svc.PlaceLimitOrder(ctx, "2", exchange.Sell, d("2"), d("50"))
	assert.NoError(t, err)
	assert.NotEmpty(t, orderID1)

	orderID2, err := svc.PlaceLimitOrder(ctx, "1", exchange.Buy, d("2"), d("50"))
	assert.NoError(t, err)
	assert.NotEmpty(t, orderID2)

	order, _ := repo.GetOrder(ctx, orderID1)
	assert.True(t, order.Remaining.Equal(decimal.Zero))
	assert.Equal(t, exchange.Matched, order.Status)

	order, _ = repo.GetOrder(ctx, orderID2)
	assert.True(t, order.Remaining.Equal(decimal.Zero))
	assert.Equal(t, exchange.Matched, order.Status)

	b, _ := w.Balance(ctx, "1", "A")
	assert.True(t, b.Equal(d("9900")))
	b, _ = w.Balance(ctx, "1", "B")
	assert.True(t, b.Equal(d("49.875")))

	b, _ = w.Balance(ctx, "2", "A")
	assert.True(t, b.Equal(d("99.75")))
	b, _ = w.Balance(ctx, "2", "B")
	assert.True(t, b.Equal(d("9950")))
}

func TestExchangeSell1(t *testing.T) {
	repo := new(memoryExchangeRepository)
	walletRepo := new(memoryWalletRepository)
	w := wallet.New(walletRepo)
	svc := exchange.New(repo, w, currency)

	w.Add(ctx, "1", "A", d("10000"))
	w.Add(ctx, "2", "B", d("10000"))

	orderID1, err := svc.PlaceLimitOrder(ctx, "1", exchange.Buy, d("2"), d("50"))
	assert.NoError(t, err)
	assert.NotEmpty(t, orderID1)

	orderID2, err := svc.PlaceLimitOrder(ctx, "2", exchange.Sell, d("2"), d("50"))
	assert.NoError(t, err)
	assert.NotEmpty(t, orderID2)

	order, _ := repo.GetOrder(ctx, orderID1)
	assert.True(t, order.Remaining.Equal(decimal.Zero))
	assert.Equal(t, exchange.Matched, order.Status)

	order, _ = repo.GetOrder(ctx, orderID2)
	assert.True(t, order.Remaining.Equal(decimal.Zero))
	assert.Equal(t, exchange.Matched, order.Status)

	b, _ := w.Balance(ctx, "1", "A")
	assert.True(t, b.Equal(d("9900")))
	b, _ = w.Balance(ctx, "1", "B")
	assert.True(t, b.Equal(d("49.875")))

	b, _ = w.Balance(ctx, "2", "A")
	assert.True(t, b.Equal(d("99.75")))
	b, _ = w.Balance(ctx, "2", "B")
	assert.True(t, b.Equal(d("9950")))
}
