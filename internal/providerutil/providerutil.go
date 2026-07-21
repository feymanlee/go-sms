package providerutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/nyaruka/phonenumbers"
)

// Prepare validates a request and resolves its effective signature before send work starts.
func Prepare(ctx context.Context, req sms.Request, defaultSignature string, signatureRequired bool) (string, error) {
	if ctx == nil {
		return "", errors.New("sms: context is required")
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if err := req.Validate(); err != nil {
		return "", err
	}

	signature := req.SignatureRef
	if signature == "" {
		signature = defaultSignature
	}
	if signatureRequired && signature == "" {
		return "", errors.New("sms: SignatureRef is required")
	}
	return signature, nil
}

// SplitE164 splits an E.164 number into country calling code and national number.
func SplitE164(value string) (string, string, error) {
	number, err := phonenumbers.Parse(value, "")
	if err != nil {
		return "", "", err
	}
	country := strconv.FormatUint(uint64(number.GetCountryCode()), 10)
	national := strings.TrimPrefix(value, "+"+country)
	if national == value || national == "" {
		return "", "", errors.New("sms: cannot split E.164 recipient")
	}
	return country, national, nil
}

// ChinaNational returns the national component of a Chinese E.164 recipient.
func ChinaNational(value string) (string, error) {
	country, national, err := SplitE164(value)
	if err != nil {
		return "", err
	}
	if country != "86" {
		return "", errors.New("sms: provider only supports +86 recipients")
	}
	return national, nil
}

// UCloudNumber converts an E.164 recipient to UCloud's country-code format.
func UCloudNumber(value string) (string, error) {
	country, national, err := SplitE164(value)
	if err != nil {
		return "", err
	}
	return "(" + country + ")" + national, nil
}

// NewHTTPClient creates the bounded client shared by provider adapters.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

// NoRedirectClient returns a shallow client copy that treats redirects as terminal responses.
func NoRedirectClient(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

// NoRedirectTransport prevents an owning http.Client from discovering redirect targets.
func NoRedirectTransport(transport http.RoundTripper) http.RoundTripper {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return noRedirectTransport{base: transport}
}

type noRedirectTransport struct {
	base http.RoundTripper
}

func (t noRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if response != nil && response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
		clone := *response
		clone.Header = response.Header.Clone()
		clone.Header.Del("Location")
		response = &clone
	}
	return response, err
}
