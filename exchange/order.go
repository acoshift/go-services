package exchange

import (
	"time"

	"github.com/shopspring/decimal"
)

// Order type
type Order struct {
	ID         string
	UserID     string
	Side       Side
	Status     Status
	Rate       decimal.Decimal
	Value      decimal.Decimal
	Remaining  decimal.Decimal
	CreatedAt  time.Time
	MatchedAt  time.Time
	FinishedAt time.Time
}

// IsLimit returns true if order is limit order
func (x Order) IsLimit() bool {
	return !x.Rate.Equal(decimal.Zero)
}

// IsMarket returns true if order is market order
func (x Order) IsMarket() bool {
	return x.Rate.Equal(decimal.Zero)
}

// Status is order status
type Status int

// Status values
const (
	Active Status = iota
	Matched
	Cancelled
)

// Side is order side
type Side int

// Side values
const (
	Buy Side = iota
	Sell
)

// ValidSide checks is side valid
func ValidSide(side Side) bool {
	return side == Buy || side == Sell
}
