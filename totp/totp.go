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
	GenerateURI(secret string, user string, issuer string) string

	// Verify verifies password
	Verify(secret string, password string) bool
}

// New creates new TOTP service
func New() TOTP {
	return &service{}
}

type service struct{}

func (s *service) Generate() string {
	var p [10]byte
	_, err := rand.Read(p[:])
	if err != nil {
		// never error or os fail
		panic(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(p[:])
}

func (s *service) GenerateURI(secret string, user string, issuer string) string {
	c := dgoogauth.OTPConfig{Secret: secret, UTC: true}
	return c.ProvisionURIWithIssuer(user, issuer)
}

func (s *service) Verify(secret string, password string) bool {
	c := dgoogauth.OTPConfig{Secret: secret, UTC: true}
	ok, _ := c.Authenticate(password)
	return ok
}
