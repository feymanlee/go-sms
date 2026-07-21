package yunpian

import (
	"net/http"

	sms "github.com/feymanlee/go-sms"
)

const (
	non2xxMessage            = "yunpian request failed"
	providerRejectionMessage = "yunpian rejected request"
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

func internalError(message string) error {
	return &sms.SendError{Kind: sms.KindInternal, Provider: "yunpian", Message: message}
}
