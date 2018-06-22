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

	// Site returns recaptcha site key
	Site() string
}

// New creates new Recaptcha
func New(site string, secret string) Recaptcha {
	return NewWithClient(site, secret, http.DefaultClient)
}

// NewWithClient creates new Recaptcha with http client
func NewWithClient(site string, secret string, client *http.Client) Recaptcha {
	if site == "" && secret == "" {
		site, secret = testSite, testSecret
	}
	return &service{site, secret, client}
}

const verifyURL = "https://www.google.com/recaptcha/api/siteverify"

const (
	testSite   = "6LeIxAcTAAAAAJcZVRqyHh71UMIEGNQ_MXjiZKhI"
	testSecret = "6LeIxAcTAAAAAGG-vFI1TnRWxMZNFuojJ4WifJWe"
)

type service struct {
	site   string
	secret string
	client *http.Client
}

func (s *service) Verify(remoteIP string, code string) (bool, error) {
	if code == "" {
		// short-circuit
		if s.site == testSite {
			return true, nil
		}
		return false, nil
	}

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

func (s *service) Site() string {
	return s.site
}
