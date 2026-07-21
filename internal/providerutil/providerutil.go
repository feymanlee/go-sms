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
func Prepare(ctx context.Context, provider string, req sms.Request, defaultSignature string, signatureRequired bool) (string, error) {
	if ctx == nil {
		return "", &sms.SendError{Kind: sms.KindInvalidRequest, Provider: provider, Message: "context is required"}
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if err := req.Validate(); err != nil {
		return "", &sms.SendError{Kind: sms.KindInvalidRequest, Provider: provider, Message: err.Error(), Cause: err}
	}

	signature := req.SignatureRef
	if signature == "" {
		signature = defaultSignature
	}
	if signatureRequired && signature == "" {
		return "", &sms.SendError{Kind: sms.KindInvalidRequest, Provider: provider, Message: "SignatureRef is required"}
	}
	return signature, nil
}

const unknownOutcomeMessage = "provider request outcome is unknown"

// UnknownOutcome wraps an indeterminate provider failure without exposing cause text.
func UnknownOutcome(provider string, recipient sms.Recipient, cause error) error {
	return unknownOutcome(provider, "", "", cause)
}

// UnknownOutcomeWithDetails wraps an indeterminate provider failure with safe provider metadata.
func UnknownOutcomeWithDetails(provider, code, requestID string, cause error) error {
	return unknownOutcome(provider, code, requestID, cause)
}

func unknownOutcome(provider, code, requestID string, cause error) error {
	return &sms.SendError{
		Kind:      sms.KindUnknownOutcome,
		Provider:  provider,
		Code:      code,
		Message:   unknownOutcomeMessage,
		RequestID: requestID,
		Cause:     sanitizedCause{cause: cause},
	}
}

// sanitizedCause preserves errors.Is behavior without exposing the original error chain.
type sanitizedCause struct {
	cause error
}

func (e sanitizedCause) Error() string { return unknownOutcomeMessage }

func (e sanitizedCause) Is(target error) bool { return errors.Is(e.cause, target) }

// Sanitize replaces the recipient's E.164 and national forms in a message.
func Sanitize(message string, recipient sms.Recipient) string {
	value := recipient.String()
	message = strings.ReplaceAll(message, value, "[recipient]")
	if _, national, err := SplitE164(value); err == nil {
		message = strings.ReplaceAll(message, national, "[recipient]")
	}
	return message
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
