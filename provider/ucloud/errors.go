package ucloud

import (
	"strconv"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

const providerErrorMessage = "ucloud provider request failed"

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

func providerRejection(code int) error {
	return &sms.SendError{
		Kind:     sms.KindRejected,
		Provider: "ucloud",
		Code:     strconv.Itoa(code),
		Message:  providerErrorMessage,
	}
}

func internalError(message string, cause error) error {
	return &sms.SendError{
		Kind:     sms.KindInternal,
		Provider: "ucloud",
		Message:  message,
		Cause:    providerutil.OpaqueCause(cause),
	}
}
