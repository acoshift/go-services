package exchange

import (
	"time"

	"github.com/shopspring/decimal"
)

// Order type
type Order struct {
	UserID    string
	Side      Side
	Rate      decimal.Decimal
	Value     decimal.Decimal
	Remaining decimal.Decimal
	CreatedAt time.Time
	MatchedAt time.Time
}

// Side is order side
type Side int

// Side values
const (
	Bid Side = iota
	Offer
)
