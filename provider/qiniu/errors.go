package qiniu

import (
	"net/http"

	"github.com/feymanlee/go-sms/failure"
)

func httpErrorCategory(status int) failure.Category {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure.Authentication
	case status == http.StatusTooManyRequests:
		return failure.RateLimited
	case status >= http.StatusInternalServerError && status <= 599:
		return failure.Unavailable
	default:
		return failure.Rejected
	}
}
