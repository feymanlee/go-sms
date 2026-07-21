package ucloud

import (
	"strconv"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

func httpErrorKind(status int) sms.ErrorKind {
	switch {
	case status == 401 || status == 403:
		return sms.KindAuthentication
	case status == 429:
		return sms.KindRateLimited
	case status >= 500 && status <= 599:
		return sms.KindUnavailable
	default:
		return sms.KindRejected
	}
}

func providerRejection(code int, message string, recipient sms.Recipient) error {
	return &sms.SendError{
		Kind:     sms.KindRejected,
		Provider: "ucloud",
		Code:     strconv.Itoa(code),
		Message:  providerutil.Sanitize(message, recipient),
	}
}

func internalError(message string, cause error) error {
	return &sms.SendError{
		Kind:     sms.KindInternal,
		Provider: "ucloud",
		Message:  message,
		Cause:    cause,
	}
}
