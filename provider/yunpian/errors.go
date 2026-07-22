package yunpian

import (
	"net/http"

	"github.com/feymanlee/go-sms/failure"
)

func httpErrorCategory(status int) (failure.Category, bool) {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure.Authentication, true
	case status == http.StatusTooManyRequests:
		return failure.RateLimited, true
	case status >= http.StatusBadRequest && status <= 499:
		return failure.Rejected, true
	case status >= http.StatusInternalServerError && status <= 599:
		return failure.Unavailable, true
	default:
		return "", false
	}
}
