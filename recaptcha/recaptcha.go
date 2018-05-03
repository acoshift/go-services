package recaptcha

import (
	"bytes"
	"io"
	"net/http"
	"net/url"

	"github.com/tidwall/gjson"
)

// Recaptcha uses to verify google recaptcha
type Recaptcha interface {
	// Verify verifies recaptcha with google server
	Verify(remoteIP string, code string) (bool, error)
}

// New creates new Recaptcha
func New(secret string) Recaptcha {
	return NewWithClient(secret, http.DefaultClient)
}

// NewWithClient creates new Recaptcha with http client
func NewWithClient(secret string, client *http.Client) Recaptcha {
	return &service{secret, client}
}

const verifyURL = "https://www.google.com/recaptcha/api/siteverify"

type service struct {
	secret string
	client *http.Client
}

func (s *service) Verify(remoteIP string, code string) (bool, error) {
	v := make(url.Values)
	v.Set("secret", s.secret)
	v.Set("remoteip", remoteIP)
	v.Set("response", code)

	resp, err := s.client.PostForm(verifyURL, v)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	io.Copy(&buf, resp.Body)

	return gjson.GetBytes(buf.Bytes(), "success").Bool(), nil
}
