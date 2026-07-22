package ucloud

import (
	"github.com/feymanlee/go-sms/failure"
)

func httpErrorCategory(status int) (failure.Category, bool) {
	switch {
	case status == 401 || status == 403:
		return failure.Authentication, true
	case status == 429:
		return failure.RateLimited, true
	case status >= 400 && status <= 499:
		return failure.Rejected, true
	case status >= 500 && status <= 599:
		return failure.Unavailable, true
	default:
		return "", false
	}
}
