package qiniu

import (
	"net/http"
	"strconv"

	sms "github.com/feymanlee/go-sms"
)

const non2xxMessage = "qiniu request failed"

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

func providerError(status int, requestID string) error {
	return &sms.SendError{
		Kind:      httpErrorKind(status),
		Provider:  "qiniu",
		Code:      strconv.Itoa(status),
		Message:   non2xxMessage,
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
