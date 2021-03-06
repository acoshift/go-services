package exchange

import (
	"time"

	"github.com/shopspring/decimal"
)

// Order type
type Order struct {
	ID         string
	UserID     string
	Type       Type
	Side       Side
	Status     Status
	Rate       decimal.Decimal
	Value      decimal.Decimal
	Remaining  decimal.Decimal
	CreatedAt  time.Time
	MatchedAt  time.Time
	FinishedAt time.Time
}

// Type is order type
type Type int

// Type values
const (
	Limit Type = iota
	Market
)

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
