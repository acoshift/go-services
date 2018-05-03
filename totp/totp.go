package totp

import (
	"crypto/rand"
	"encoding/base32"

	"github.com/dgryski/dgoogauth"
)

// TOTP is a Time-based One-time Password service
type TOTP interface {
	// Generate generates new OTP secret
	Generate() string

	// GenerateURI generates uri from secret
	GenerateURI(secret string, user string) string

	// GenerateURIWithIssuer generates uri with issuer
	GenerateURIWithIssuer(secret string, user string, issuer string) string

	// Verify verifies password
	Verify(secret string, password string) bool
}

// New creates new TOTP service
func New() TOTP {
	return &service{}
}

// NewWithIssuer creates new TOTP service with default issuer
func NewWithIssuer(issuer string) TOTP {
	return &service{issuer}
}

type service struct {
	issuer string
}

func (s *service) Generate() string {
	var p [10]byte
	_, err := rand.Read(p[:])
	if err != nil {
		// never error or os fail
		panic(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(p[:])
}

func (s *service) GenerateURI(secret string, user string) string {
	return s.GenerateURIWithIssuer(secret, user, s.issuer)
}

func (s *service) GenerateURIWithIssuer(secret string, user string, issuer string) string {
	c := dgoogauth.OTPConfig{Secret: secret, UTC: true}
	if issuer == "" {
		return c.ProvisionURI(user)
	}
	return c.ProvisionURIWithIssuer(user, issuer)
}

func (s *service) Verify(secret string, password string) bool {
	c := dgoogauth.OTPConfig{Secret: secret, UTC: true}
	ok, _ := c.Authenticate(password)
	return ok
}
