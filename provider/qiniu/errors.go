package qiniu

import (
	"net/http"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

func httpErrorKind(status int) sms.ErrorKind {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return sms.KindAuthentication
	case status == http.StatusTooManyRequests:
		return sms.KindRateLimited
	case status >= http.StatusInternalServerError && status <= 599:
		return sms.KindUnavailable
	default:
		return sms.KindRejected
	}
}

func providerError(status int, message, requestID string, recipient sms.Recipient) error {
	return &sms.SendError{
		Kind:      httpErrorKind(status),
		Provider:  "qiniu",
		Message:   providerutil.Sanitize(message, recipient),
		RequestID: requestID,
	}
}

func internalError(message, requestID string) error {
	return &sms.SendError{
		Kind:      sms.KindInternal,
		Provider:  "qiniu",
		Message:   message,
		RequestID: requestID,
	}
}
